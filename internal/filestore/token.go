package filestore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

// TokenClaims 是 Filestore 专用 JWT 合同。它与 code-session ingress claims
// 相互独立，避免 filesystem 权限扩散到 worker、relay 或 upstream proxy。
type TokenClaims struct {
	jwt.RegisteredClaims
	OrgUUID                   string `json:"org_uuid"`
	AccountUUID               string `json:"account_uuid"`
	WorkspaceUUID             string `json:"workspace_uuid"`
	WorkspaceTaggedID         string `json:"workspace_tagged_id"`
	ResolvedWorkspaceTaggedID string `json:"resolved_workspace_tagged_id"`
	FilesystemID              string `json:"filesystem_id"`
	// OrgTaints 保存签发时的组织级安全与合规策略标签。鉴权时按集合与数据库现值比对，
	// 用于识别过期的策略快照；标签本身不在此处定义具体的文件操作权限。
	OrgTaints []string `json:"org_taints"`
	// WorkspaceCMEKEnabled 保存签发时工作区是否配置了客户管理密钥（CMEK）。
	// 它只描述配置状态，不代表某次 S3 请求已经设置或执行了服务端加密。
	WorkspaceCMEKEnabled bool `json:"workspace_cmek_enabled"`
	// Readonly 为 nil 表示第一类读写 token；值为 true 时表示第二类只读 token。
	Readonly *bool `json:"readonly,omitempty"`
}

// UnmarshalJSON 把可接受的 payload 收窄到已知合同，避免旧凭证或其他 JWT
// 即使恰好由同一把密钥签名，也因附带额外 claims 而被 Filestore 接受。
func (c *TokenClaims) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	required := []string{
		"sub",
		"org_uuid",
		"account_uuid",
		"workspace_uuid",
		"workspace_tagged_id",
		"resolved_workspace_tagged_id",
		"filesystem_id",
		"org_taints",
		"workspace_cmek_enabled",
	}
	allowed := make(map[string]struct{}, len(required)+1)
	for _, name := range required {
		allowed[name] = struct{}{}
		if _, exists := fields[name]; !exists {
			return fmt.Errorf("filestore token is missing claim %q", name)
		}
	}
	allowed["readonly"] = struct{}{}
	for name := range fields {
		if _, exists := allowed[name]; !exists {
			return fmt.Errorf("filestore token contains unsupported claim %q", name)
		}
	}
	var orgTaints []string
	if err := json.Unmarshal(fields["org_taints"], &orgTaints); err != nil || orgTaints == nil {
		return errors.New("filestore org_taints claim must be an array")
	}
	var workspaceCMEKEnabled bool
	if err := json.Unmarshal(fields["workspace_cmek_enabled"], &workspaceCMEKEnabled); err != nil || string(fields["workspace_cmek_enabled"]) == "null" {
		return errors.New("filestore workspace_cmek_enabled claim must be a boolean")
	}
	if readonlyJSON, exists := fields["readonly"]; exists {
		var readonly bool
		if err := json.Unmarshal(readonlyJSON, &readonly); err != nil || !readonly {
			return errors.New("filestore readonly claim must be true when present")
		}
	}
	type plainTokenClaims TokenClaims
	var claims plainTokenClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return err
	}
	*c = TokenClaims(claims)
	return nil
}

// TokenIdentity 是签发 Filestore JWT 所需的完整、已解析身份快照。
// 调用方应从可信数据库上下文构造，不能直接采用客户端提交的同名字段。
type TokenIdentity struct {
	Subject                   string
	OrgUUID                   string
	AccountUUID               string
	WorkspaceUUID             string
	WorkspaceTaggedID         string
	ResolvedWorkspaceTaggedID string
	FilesystemID              string
	// OrgTaints 与 WorkspaceCMEKEnabled 来自可信数据库上下文，签入 JWT 后构成安全策略快照。
	OrgTaints            []string
	WorkspaceCMEKEnabled bool
}

// TokenCredentials 持有 Filestore JWT 的 Ed25519 签发与验签密钥。
type TokenCredentials struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	kid        string
}

var processFilestoreTokenCredentials struct {
	once        sync.Once
	credentials *TokenCredentials
	err         error
}

// NewTokenCredentials 从与 session ingress 相同的持久化 Ed25519 私钥文件加载密钥，
// 但使用独立的 claims、issuer 与 audience；开发环境则复用一套进程级临时 Filestore 密钥。
func NewTokenCredentials(cfg config.Config) (*TokenCredentials, error) {
	keyFile := strings.TrimSpace(cfg.CodeSession.JWTSigningPrivateKeyFile)
	if keyFile != "" {
		privateKey, err := readFilestoreTokenPrivateKey(keyFile)
		if err != nil {
			return nil, err
		}
		return newTokenCredentials(privateKey), nil
	}
	if cfg.Env == config.EnvironmentProd {
		return nil, errors.New("code_session.jwt_signing_private_key_file is required for filestore tokens when env is prod")
	}
	processFilestoreTokenCredentials.once.Do(func() {
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			processFilestoreTokenCredentials.err = fmt.Errorf("generate process filestore signing key: %w", err)
			return
		}
		processFilestoreTokenCredentials.credentials = newTokenCredentials(privateKey)
	})
	return processFilestoreTokenCredentials.credentials, processFilestoreTokenCredentials.err
}

func newTokenCredentials(privateKey ed25519.PrivateKey) *TokenCredentials {
	publicKey := privateKey.Public().(ed25519.PublicKey)
	fingerprint := sha256.Sum256(publicKey)
	return &TokenCredentials{
		privateKey: privateKey,
		publicKey:  publicKey,
		kid:        "ed25519-" + base64.RawURLEncoding.EncodeToString(fingerprint[:]),
	}
}

func readFilestoreTokenPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read filestore signing key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("filestore signing key must contain one PKCS#8 PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse filestore PKCS#8 signing key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("filestore signing key must be an Ed25519 private key")
	}
	return privateKey, nil
}

// Issue 签发第一类读写 token；rclone 将返回值原样放入 Authorization Bearer header。
func (c *TokenCredentials) Issue(identity TokenIdentity) (string, error) {
	return c.issue(identity, nil)
}

// IssueReadonly 签发第二类只读 token。readonly 由签发器固定为 true，
// 不把布尔值暴露给调用方，以免产生“携带 readonly=false 的第二类 token”。
func (c *TokenCredentials) IssueReadonly(identity TokenIdentity) (string, error) {
	readonly := true
	return c.issue(identity, &readonly)
}

func (c *TokenCredentials) issue(identity TokenIdentity, readonly *bool) (string, error) {
	if c == nil || len(c.privateKey) == 0 {
		return "", errors.New("filestore credential signer is not configured")
	}
	identity, err := normalizeTokenIdentity(identity)
	if err != nil {
		return "", err
	}
	claims := TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: identity.Subject,
		},
		OrgUUID:                   identity.OrgUUID,
		AccountUUID:               identity.AccountUUID,
		WorkspaceUUID:             identity.WorkspaceUUID,
		WorkspaceTaggedID:         identity.WorkspaceTaggedID,
		ResolvedWorkspaceTaggedID: identity.ResolvedWorkspaceTaggedID,
		FilesystemID:              identity.FilesystemID,
		OrgTaints:                 identity.OrgTaints,
		WorkspaceCMEKEnabled:      identity.WorkspaceCMEKEnabled,
		Readonly:                  copyBool(readonly),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = c.kid
	signed, err := token.SignedString(c.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign filestore token: %w", err)
	}
	return signed, nil
}

// Verify 固定 EdDSA、kid 与严格 JWT 解码，并校验全部 Filestore claims。
func (c *TokenCredentials) Verify(rawToken string) (TokenClaims, error) {
	if c == nil || len(c.publicKey) == 0 {
		return TokenClaims{}, errors.New("filestore credential verifier is not configured")
	}
	claims := TokenClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithStrictDecoding(),
	)
	token, err := parser.ParseWithClaims(strings.TrimSpace(rawToken), &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodEdDSA || token.Header["typ"] != "JWT" {
			return nil, errors.New("invalid filestore signing algorithm")
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || subtle.ConstantTimeCompare([]byte(kid), []byte(c.kid)) != 1 {
			return nil, errors.New("invalid filestore signing key")
		}
		return c.publicKey, nil
	})
	if err != nil {
		return TokenClaims{}, fmt.Errorf("verify filestore token: %w", err)
	}
	if !token.Valid {
		return TokenClaims{}, errors.New("verify filestore token: token is invalid")
	}
	if err := validateTokenClaims(claims); err != nil {
		return TokenClaims{}, err
	}
	claims.OrgTaints = canonicalOrgTaints(claims.OrgTaints)
	claims.Readonly = copyBool(claims.Readonly)
	return claims, nil
}

func normalizeTokenIdentity(identity TokenIdentity) (TokenIdentity, error) {
	identity.Subject = strings.TrimSpace(identity.Subject)
	identity.OrgUUID = strings.TrimSpace(identity.OrgUUID)
	identity.AccountUUID = strings.TrimSpace(identity.AccountUUID)
	identity.WorkspaceUUID = strings.TrimSpace(identity.WorkspaceUUID)
	identity.WorkspaceTaggedID = strings.TrimSpace(identity.WorkspaceTaggedID)
	identity.ResolvedWorkspaceTaggedID = strings.TrimSpace(identity.ResolvedWorkspaceTaggedID)
	identity.FilesystemID = strings.TrimSpace(identity.FilesystemID)
	identity.OrgTaints = canonicalOrgTaints(identity.OrgTaints)
	if err := validateTokenIdentity(identity); err != nil {
		return TokenIdentity{}, err
	}
	return identity, nil
}

func validateTokenClaims(claims TokenClaims) error {
	if err := validateTokenIdentity(TokenIdentity{
		Subject:                   claims.Subject,
		OrgUUID:                   claims.OrgUUID,
		AccountUUID:               claims.AccountUUID,
		WorkspaceUUID:             claims.WorkspaceUUID,
		WorkspaceTaggedID:         claims.WorkspaceTaggedID,
		ResolvedWorkspaceTaggedID: claims.ResolvedWorkspaceTaggedID,
		FilesystemID:              claims.FilesystemID,
		OrgTaints:                 claims.OrgTaints,
		WorkspaceCMEKEnabled:      claims.WorkspaceCMEKEnabled,
	}); err != nil {
		return err
	}
	if claims.Readonly != nil && !*claims.Readonly {
		return errors.New("invalid filestore readonly claim")
	}
	return nil
}

func validateTokenIdentity(identity TokenIdentity) error {
	values := []string{
		identity.Subject,
		identity.OrgUUID,
		identity.AccountUUID,
		identity.WorkspaceUUID,
		identity.WorkspaceTaggedID,
		identity.ResolvedWorkspaceTaggedID,
		identity.FilesystemID,
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return errors.New("filestore credential identity is incomplete")
		}
	}
	for _, taint := range identity.OrgTaints {
		if strings.TrimSpace(taint) == "" {
			return errors.New("filestore credential organization taint is invalid")
		}
	}
	return nil
}

func canonicalOrgTaints(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// OrgTaintsEqual 以签发时的规范化规则比较两份组织 taints，
// 避免数据库 JSON 数组顺序影响同一策略的身份判定。
func OrgTaintsEqual(left, right []string) bool {
	left = canonicalOrgTaints(left)
	right = canonicalOrgTaints(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
