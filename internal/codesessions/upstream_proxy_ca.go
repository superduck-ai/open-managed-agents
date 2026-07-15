package codesessions

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"golang.org/x/sync/singleflight"
)

const (
	// 根证书每次服务启动都会使用外部稳定私钥重新签发。把有效期固定为一年，
	// 可以避免部署侧长期保留一张会悄然过期的静态证书，同时限制单张信任锚的生命周期。
	upstreamProxyRootValidity = 365 * 24 * time.Hour
	// leaf 最长有效 24 小时，但缓存只复用 12 小时，使正常流量在证书到期前主动轮换。
	upstreamProxyLeafValidity     = 24 * time.Hour
	upstreamProxyLeafRefreshAfter = 12 * time.Hour
	// 缓存容量是进程级上限，防止攻击者通过不断请求不同域名造成服务端内存无界增长。
	maxUpstreamProxyLeafCacheSize = 1024
)

// upstreamProxyRuntime 把 CCR 出口的拨号与 CA 状态收拢在一起，避免 Handler 直接持有一组容易失配的 TLS 字段。
// dialTLS 的合同是返回已经完成握手且验证过真实上游证书的连接；函数字段同时保留无外网 MITM 单测注入点。
type upstreamProxyRuntime struct {
	dial    func(context.Context, string) (net.Conn, error)
	dialTLS func(context.Context, string, string) (net.Conn, error)
	caOnce  sync.Once
	ca      *upstreamProxyCertificateAuthority
	caErr   error
}

// upstreamProxyCertificateAuthority 保存本次启动签发的 MITM 根证书、由稳定私钥构造的签名器，
// 以及进程内 leaf 缓存。稳定私钥由部署侧文件提供并加载到 API server 内存；对外接口只返回
// certificatePEM，绝不暴露 signer 或私钥字节。
type upstreamProxyCertificateAuthority struct {
	// certificate 是已解析的根证书，用于构造 leaf 的 issuer、限制 leaf 到期时间并拼接证书链。
	certificate *x509.Certificate
	// certificatePEM 是允许公开下载的根证书内容，不包含任何私钥材料。
	certificatePEM []byte
	// signer 持有根私钥，只用于给动态 leaf 签名。
	signer crypto.Signer
	// leafCache 使用固定容量 LRU：热门域名会持续复用证书，冷门域名在容量达到上限后自动淘汰，
	// 从而同时避免无界内存增长和旧 map 满载后新域名永远无法进入缓存的问题。
	leafCache *lru.Cache[string, upstreamProxyCachedLeaf]
	// leafSigningGroup 只合并同一规范化域名的并发签发；不同域名拥有不同 key，可以并行生成密钥和签名。
	// LRU 自身的锁只覆盖短暂的缓存操作，耗时的证书生成不会占用全局锁。
	leafSigningGroup singleflight.Group
}

// upstreamProxyCachedLeaf 保存可直接交给 tls.Config 的完整 leaf，以及主动轮换时间。
// refreshAt 是缓存策略时间，不等同于 X.509 NotAfter；到达 refreshAt 后会重新签发并覆盖旧值。
type upstreamProxyCachedLeaf struct {
	certificate tls.Certificate
	refreshAt   time.Time
}

// newUpstreamProxyRuntime 安装生产拨号实现；测试可以替换函数字段，在不访问公网的情况下验证 MITM 流程。
func newUpstreamProxyRuntime() upstreamProxyRuntime {
	return upstreamProxyRuntime{
		dial:    dialUpstreamProxyTarget,
		dialTLS: dialUpstreamProxyTLSTarget,
	}
}

// loadUpstreamProxyCA 在 Handler 生命周期内只加载或生成一次 CA。
// sync.Once 同时缓存成功结果和失败结果，避免并发请求重复读取私钥文件或生成不同的临时信任锚。
func (h *Handler) loadUpstreamProxyCA() (*upstreamProxyCertificateAuthority, error) {
	// 配置稳定私钥时在启动期签发根证书；未配置且 MITM 关闭时保留临时 CA，
	// 兼容只要求 CA 下载成功的旧透传模式。
	h.upstreamProxy.caOnce.Do(func() {
		h.upstreamProxy.ca, h.upstreamProxy.caErr = loadOrGenerateUpstreamProxyCA(h.cfg)
	})
	return h.upstreamProxy.ca, h.upstreamProxy.caErr
}

// loadOrGenerateUpstreamProxyCA 根据配置选择稳定私钥驱动的启动期 CA 或临时 CA。
// 配置稳定私钥时，根证书在启动期签发并仅保存在内存；MITM 关闭且未配置私钥时，
// 仍允许生成进程生命周期内的临时 CA，以兼容 CA 下载合同。
func loadOrGenerateUpstreamProxyCA(cfg config.Config) (*upstreamProxyCertificateAuthority, error) {
	keyFile := strings.TrimSpace(cfg.CodeSessionUpstreamProxyCAKeyFile)
	if keyFile == "" {
		if cfg.CodeSessionUpstreamProxyMITMEnabled {
			return nil, errors.New("MITM CA private key is required")
		}
		return generateUpstreamProxyCertificateAuthority()
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read MITM CA private key: %w", err)
	}
	signer, err := parseUpstreamProxyCAPrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	return issueUpstreamProxyCertificateAuthority(signer, time.Now().UTC().Truncate(time.Second))
}

// parseUpstreamProxyCAPrivateKey 只接受单个、未加密的 PEM 私钥。
// 部署中既可能使用 OpenSSL 常见的 SEC1 EC/PKCS#1 RSA，也可能使用统一的 PKCS#8，
// 因此显式支持三种编码，最终再收敛为 x509.CreateCertificate 所需的 crypto.Signer。
func parseUpstreamProxyCAPrivateKey(keyPEM []byte) (crypto.Signer, error) {
	block, rest := pem.Decode(keyPEM)
	if block == nil || strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("MITM CA private key file must contain exactly one PEM block")
	}
	if len(block.Headers) != 0 {
		return nil, errors.New("encrypted MITM CA private keys are not supported")
	}

	var (
		privateKey any
		err        error
	)
	switch block.Type {
	case "PRIVATE KEY":
		privateKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		privateKey, err = x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported MITM CA private key PEM type %q", block.Type)
	}
	if err != nil {
		return nil, errors.New("MITM CA private key is invalid")
	}
	signer, ok := privateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("MITM CA private key cannot sign certificates")
	}
	return signer, nil
}

// generateUpstreamProxyCertificateAuthority 创建进程生命周期内的 ECDSA P-256 临时 CA。
// 它只服务于未启用 MITM 时的兼容路径；正式 MITM 必须使用所有实例共享的外部稳定私钥。
func generateUpstreamProxyCertificateAuthority() (*upstreamProxyCertificateAuthority, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return issueUpstreamProxyCertificateAuthority(privateKey, time.Now().UTC().Truncate(time.Second))
}

// issueUpstreamProxyCertificateAuthority 用指定稳定私钥自签一张新的根证书。
// 每次签发的序列号、有效期和证书签名都会变化，但 Subject DER、公钥与 SKI 保持稳定。因此服务重启后，
// 新 leaf 的 issuer 和签名仍可直接链到 Claude 已信任且尚未过期的旧根证书，不要求证书字节完全相同。
func issueUpstreamProxyCertificateAuthority(signer crypto.Signer, now time.Time) (*upstreamProxyCertificateAuthority, error) {
	serialNumber, err := randomUpstreamProxySerialNumber()
	if err != nil {
		return nil, err
	}
	keyID, err := upstreamProxySubjectKeyID(signer.Public())
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Open Managed Agents"},
			CommonName:   "Open Managed Agents CCRv2 MITM CA",
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(upstreamProxyRootValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SubjectKeyId:          append([]byte(nil), keyID...),
		AuthorityKeyId:        append([]byte(nil), keyID...),
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, signer.Public(), signer)
	if err != nil {
		return nil, fmt.Errorf("sign MITM CA certificate: %w", err)
	}
	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		return nil, fmt.Errorf("parse generated MITM CA certificate: %w", err)
	}
	if err := certificate.CheckSignatureFrom(certificate); err != nil {
		return nil, fmt.Errorf("verify generated MITM CA certificate: %w", err)
	}
	return newUpstreamProxyCertificateAuthority(certificate, signer)
}

// upstreamProxySubjectKeyID 按 RFC 5280 常用的 method 1 计算 SKI：对 SubjectPublicKey
// BIT STRING 的实际公钥字节做 SHA-1。这里的 SHA-1 只生成非安全敏感的证书链标识符，
// 不承担签名或完整性保护；显式采用该算法是为了与既有 OpenSSL 根证书保持相同 SKI。
func upstreamProxySubjectKeyID(publicKey any) ([]byte, error) {
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal MITM CA public key: %w", err)
	}
	var publicKeyInfo struct {
		Algorithm        pkix.AlgorithmIdentifier
		SubjectPublicKey asn1.BitString
	}
	rest, err := asn1.Unmarshal(publicKeyDER, &publicKeyInfo)
	if err != nil || len(rest) != 0 {
		return nil, errors.New("parse MITM CA public key identifier")
	}
	keyID := sha1.Sum(publicKeyInfo.SubjectPublicKey.Bytes)
	return append([]byte(nil), keyID[:]...), nil
}

// newUpstreamProxyCertificateAuthority 创建 CA 运行时，并在入口处固定 leaf 缓存容量。
// lru.Cache 自身并发安全，因此调用方无需再为 Get/Add 包一层覆盖证书签发过程的全局互斥锁。
func newUpstreamProxyCertificateAuthority(certificate *x509.Certificate, signer crypto.Signer) (*upstreamProxyCertificateAuthority, error) {
	leafCache, err := lru.New[string, upstreamProxyCachedLeaf](maxUpstreamProxyLeafCacheSize)
	if err != nil {
		return nil, fmt.Errorf("create MITM leaf certificate cache: %w", err)
	}
	return &upstreamProxyCertificateAuthority{
		certificate:    certificate,
		certificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}),
		signer:         signer,
		leafCache:      leafCache,
	}, nil
}

// certificateForHost 返回目标主机可用的 MITM leaf。
// 主机名先统一规范化，随后走 LRU 快路径；只有 cache miss 或到达 refreshAt 才进入按主机键控的 singleflight。
func (ca *upstreamProxyCertificateAuthority) certificateForHost(host string, now time.Time) (*tls.Certificate, error) {
	host = canonicalUpstreamProxyHostname(host)
	if host == "" {
		return nil, errors.New("invalid MITM certificate hostname")
	}
	if certificate, ok := ca.cachedLeafCertificate(host, now); ok {
		return &certificate, nil
	}

	// 快路径 miss 后可能已有另一个请求完成签发，因此进入 singleflight 后必须再次检查缓存。
	// 不在闭包外删除过期项：否则旧请求可能在并发签发完成后误删刚写入的新证书。
	value, err, _ := ca.leafSigningGroup.Do(host, func() (any, error) {
		if certificate, ok := ca.cachedLeafCertificate(host, now); ok {
			return certificate, nil
		}
		certificate, generateErr := ca.generateLeafCertificate(host, now)
		if generateErr != nil {
			return nil, generateErr
		}
		ca.leafCache.Add(host, upstreamProxyCachedLeaf{
			certificate: certificate,
			refreshAt:   now.Add(upstreamProxyLeafRefreshAfter),
		})
		return certificate, nil
	})
	if err != nil {
		return nil, err
	}
	certificate, ok := value.(tls.Certificate)
	if !ok {
		return nil, fmt.Errorf("unexpected MITM leaf certificate result type %T", value)
	}
	// singleflight 会让等待者共享 value；返回结构体副本，避免不同 TLS 配置共享可变的 tls.Certificate 外壳。
	return &certificate, nil
}

// cachedLeafCertificate 只判断缓存项是否仍处于主动复用窗口，不负责删除过期项。
// 延迟到 singleflight 内生成并 Add 覆盖，可以避免一个较慢请求误删另一个请求刚写入的新证书。
func (ca *upstreamProxyCertificateAuthority) cachedLeafCertificate(host string, now time.Time) (tls.Certificate, bool) {
	cached, ok := ca.leafCache.Get(host)
	if !ok || !now.Before(cached.refreshAt) {
		return tls.Certificate{}, false
	}
	return cached.certificate, true
}

// generateLeafCertificate 为单个规范化 DNS 名或 IP 地址生成独立的 ECDSA P-256 私钥和服务端证书。
// 每张 leaf 都由当前 CA 签发，且有效期绝不会超过根证书的 NotAfter。
func (ca *upstreamProxyCertificateAuthority) generateLeafCertificate(host string, now time.Time) (tls.Certificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serialNumber, err := randomUpstreamProxySerialNumber()
	if err != nil {
		return tls.Certificate{}, err
	}
	// 根 CA 临近到期时缩短 leaf 有效期，防止签发一个生命周期越过 issuer 的证书。
	notAfter := now.Add(upstreamProxyLeafValidity)
	if notAfter.After(ca.certificate.NotAfter) {
		notAfter = ca.certificate.NotAfter
	}
	if !notAfter.After(now.Add(5 * time.Minute)) {
		return tls.Certificate{}, errors.New("MITM CA expires too soon to issue a leaf certificate")
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: host},
		// 回拨五分钟容忍 sandbox 与 API server 之间的小幅时钟偏差。
		NotBefore:      now.Add(-5 * time.Minute),
		NotAfter:       notAfter,
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		AuthorityKeyId: append([]byte(nil), ca.certificate.SubjectKeyId...),
	}
	// IP 目标必须写入 IP SAN；其余目标写入 DNS SAN。现代 TLS 校验不会只依赖 CommonName。
	if address, err := netip.ParseAddr(host); err == nil {
		template.IPAddresses = []net.IP{net.IP(address.AsSlice())}
	} else {
		template.DNSNames = []string{host}
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, ca.certificate, &privateKey.PublicKey, ca.signer)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		return tls.Certificate{}, err
	}
	// tls.Certificate 的链顺序为 leaf 在前、issuer 在后；PrivateKey 仅属于本张动态 leaf。
	return tls.Certificate{
		Certificate: [][]byte{certificateDER, ca.certificate.Raw},
		PrivateKey:  privateKey,
		Leaf:        leaf,
	}, nil
}

// randomUpstreamProxySerialNumber 在 [1, 2^128-1] 内生成正序列号，既满足 X.509 要求，也降低并发签发的碰撞概率。
func randomUpstreamProxySerialNumber() (*big.Int, error) {
	upperBound := new(big.Int).Lsh(big.NewInt(1), 128)
	upperBound.Sub(upperBound, big.NewInt(1))
	serialNumber, err := rand.Int(rand.Reader, upperBound)
	if err != nil {
		return nil, err
	}
	return serialNumber.Add(serialNumber, big.NewInt(1)), nil
}

// canonicalUpstreamProxyHostname 生成缓存与 singleflight 共用的稳定 key：去空白、转小写、去结尾点，
// 并把带方括号或 IPv4-mapped IPv6 形式的 IP 统一为 netip 的标准文本表示。
func canonicalUpstreamProxyHostname(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return address.Unmap().String()
	}
	return host
}
