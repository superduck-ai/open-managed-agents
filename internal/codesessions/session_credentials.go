package codesessions

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// OAuth-compatible token 只在 OMA 内部代理中使用，不代表 Anthropic 官方 OAuth token。
	oauthCompatibleTokenPrefix = "sk-ant-oat01-"
	// session-ingress token 在前缀后承载 Ed25519 JWT，供 worker、relay 和 upstream proxy 使用。
	sessionIngressTokenPrefix = "sk-ant-si-"
	sessionIngressIssuer      = "session-ingress"
	sessionIngressAudience    = "anthropic-api"
)

// SessionCredentialClaims 描述写入 session-ingress JWT 的稳定身份和租户关联。
type SessionCredentialClaims struct {
	jwt.RegisteredClaims
	SessionID        string `json:"session_id"`
	PublicSessionID  string `json:"public_session_id"`
	AgentID          string `json:"agent_id"`
	AgentVersion     int    `json:"agent_version"`
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	Application      string `json:"application"`
	Role             string `json:"role"`
	AccountEmail     string `json:"account_email,omitempty"`
}

// SessionCredentialIdentity 是签发输入；调用方必须从 active code session 数据库记录构造。
type SessionCredentialIdentity struct {
	SessionID        string
	PublicSessionID  string
	AgentID          string
	AgentVersion     int
	OrganizationUUID string
	WorkspaceUUID    string
	AccountEmail     string
}

// SessionCredentials 持有同一进程使用的 Ed25519 签发与验证密钥。
type SessionCredentials struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	kid        string
	now        func() time.Time
}

var processSessionCredentials struct {
	once        sync.Once
	credentials *SessionCredentials
	err         error
}

// NewSessionCredentials 在生产环境强制读取持久化 PKCS#8 私钥；开发和测试环境
// 复用进程级临时密钥，保证同一进程内 API server 与 environment runner 可以互验 JWT。
func NewSessionCredentials(cfg config.Config) (*SessionCredentials, error) {
	keyFile := strings.TrimSpace(cfg.CodeSessionJWTSigningKeyFile)
	if keyFile != "" {
		privateKey, err := readSessionCredentialPrivateKey(keyFile)
		if err != nil {
			return nil, err
		}
		return newSessionCredentials(privateKey, time.Now), nil
	}
	if productionEnvironment(cfg.AppEnv) {
		return nil, errors.New("CODE_SESSION_JWT_SIGNING_KEY_FILE is required in production")
	}
	processSessionCredentials.once.Do(func() {
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			processSessionCredentials.err = fmt.Errorf("generate process code-session signing key: %w", err)
			return
		}
		processSessionCredentials.credentials = newSessionCredentials(privateKey, time.Now)
	})
	return processSessionCredentials.credentials, processSessionCredentials.err
}

func newSessionCredentials(privateKey ed25519.PrivateKey, now func() time.Time) *SessionCredentials {
	publicKey := privateKey.Public().(ed25519.PublicKey)
	fingerprint := sha256.Sum256(publicKey)
	// kid 由公钥指纹确定，验证时必须与当前签发器完全一致，避免无意接受其他 Ed25519 key。
	return &SessionCredentials{
		privateKey: privateKey,
		publicKey:  publicKey,
		kid:        "ed25519-" + base64.RawURLEncoding.EncodeToString(fingerprint[:]),
		now:        now,
	}
}

func readSessionCredentialPrivateKey(path string) (ed25519.PrivateKey, error) {
	// 只接受一个无尾随数据的 PKCS#8 PRIVATE KEY block，避免模糊解析多个密钥。
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read code-session signing key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("code-session signing key must contain one PKCS#8 PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse code-session PKCS#8 signing key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("code-session signing key must be an Ed25519 private key")
	}
	return privateKey, nil
}

// Issue 签发带 sk-ant-si- 前缀的 Ed25519 JWT。JWT 不设置独立 expiry；当前只通过
// 签名、固定 claims 和请求路径绑定完成鉴权，不回查 session 或 worker lease。
func (c *SessionCredentials) Issue(identity SessionCredentialIdentity) (string, error) {
	if c == nil || len(c.privateKey) == 0 {
		return "", errors.New("code-session credential signer is not configured")
	}
	if err := validateSessionCredentialIdentity(identity); err != nil {
		return "", err
	}
	now := c.now().UTC()
	jti, err := randomCredentialValue(18)
	if err != nil {
		return "", err
	}
	claims := SessionCredentialClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   sessionIngressIssuer,
			Subject:  identity.SessionID,
			Audience: jwt.ClaimStrings{sessionIngressAudience},
			IssuedAt: jwt.NewNumericDate(now),
			ID:       jti,
		},
		SessionID:        identity.SessionID,
		PublicSessionID:  identity.PublicSessionID,
		AgentID:          identity.AgentID,
		AgentVersion:     identity.AgentVersion,
		OrganizationUUID: identity.OrganizationUUID,
		WorkspaceUUID:    identity.WorkspaceUUID,
		Application:      "ccr",
		Role:             "worker",
		AccountEmail:     strings.TrimSpace(identity.AccountEmail),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	// golang-jwt 默认设置 typ=JWT；这里额外写入可验证的公钥标识。
	token.Header["kid"] = c.kid
	signed, err := token.SignedString(c.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign code-session ingress token: %w", err)
	}
	return sessionIngressTokenPrefix + signed, nil
}

// Verify 固定 EdDSA、kid、issuer、audience 和时间约束，返回已完成结构校验的 claims。
func (c *SessionCredentials) Verify(rawToken string) (SessionCredentialClaims, error) {
	if c == nil || len(c.publicKey) == 0 {
		return SessionCredentialClaims{}, errors.New("code-session credential verifier is not configured")
	}
	if !strings.HasPrefix(rawToken, sessionIngressTokenPrefix) {
		return SessionCredentialClaims{}, errors.New("invalid session ingress token prefix")
	}
	claims := SessionCredentialClaims{}
	// 显式固定算法、issuer、audience 与严格 base64 解码，拒绝算法降级和宽松 JWT。
	// 旧版本签发、仍携带 exp 的 token 继续按 golang-jwt 默认规则校验该字段。
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(sessionIngressIssuer),
		jwt.WithAudience(sessionIngressAudience),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(c.now),
		jwt.WithStrictDecoding(),
	)
	token, err := parser.ParseWithClaims(strings.TrimPrefix(rawToken, sessionIngressTokenPrefix), &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodEdDSA || token.Header["typ"] != "JWT" {
			return nil, errors.New("invalid session ingress signing algorithm")
		}
		kid, ok := token.Header["kid"].(string)
		// kid 虽不是秘密，仍使用常量时间比较保持凭证比较路径一致。
		if !ok || subtle.ConstantTimeCompare([]byte(kid), []byte(c.kid)) != 1 {
			return nil, errors.New("invalid session ingress signing key")
		}
		return c.publicKey, nil
	})
	if err != nil {
		return SessionCredentialClaims{}, fmt.Errorf("verify session ingress token: %w", err)
	}
	if !token.Valid {
		return SessionCredentialClaims{}, errors.New("verify session ingress token: token is invalid")
	}
	if err := validateSessionCredentialClaims(claims); err != nil {
		return SessionCredentialClaims{}, err
	}
	return claims, nil
}

func validateSessionCredentialIdentity(identity SessionCredentialIdentity) error {
	values := []string{
		identity.SessionID,
		identity.PublicSessionID,
		identity.AgentID,
		identity.OrganizationUUID,
		identity.WorkspaceUUID,
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return errors.New("code-session credential identity is incomplete")
		}
	}
	if identity.AgentVersion <= 0 {
		return errors.New("code-session credential agent version is invalid")
	}
	return nil
}

func validateSessionCredentialClaims(claims SessionCredentialClaims) error {
	identity := SessionCredentialIdentity{
		SessionID:        claims.SessionID,
		PublicSessionID:  claims.PublicSessionID,
		AgentID:          claims.AgentID,
		AgentVersion:     claims.AgentVersion,
		OrganizationUUID: claims.OrganizationUUID,
		WorkspaceUUID:    claims.WorkspaceUUID,
	}
	if err := validateSessionCredentialIdentity(identity); err != nil {
		return err
	}
	// sub 必须与 session_id 重合，application/role 则把 token 限定为 CCR worker 凭证。
	if claims.Subject != claims.SessionID || claims.ID == "" || claims.Application != "ccr" || claims.Role != "worker" {
		return errors.New("invalid session ingress token claims")
	}
	return nil
}

func newOAuthCompatibleToken() (string, error) {
	// token 本身是高熵本地 bearer credential，数据库仅保存其 hash。
	value, err := randomCredentialValue(32)
	if err != nil {
		return "", err
	}
	return oauthCompatibleTokenPrefix + value, nil
}

func randomCredentialValue(size int) (string, error) {
	// RawURL 编码避免文件描述符和 HTTP header 中出现填充符或需转义字符。
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate code-session credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func productionEnvironment(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "production", "prod":
		return true
	default:
		return false
	}
}
