package porttunnel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	forwardSignatureHeader = "X-OMA-Port-Signature"
	forwardRequestHeader   = "X-OMA-Port-Request"
	forwardSignatureSkew   = 30 * time.Second
	forwardSignatureScheme = "v1"
)

type ForwardSigner struct {
	key []byte
	now func() time.Time
}

func NewForwardSigner(key []byte) *ForwardSigner {
	return &ForwardSigner{key: append([]byte(nil), key...), now: time.Now}
}

func (s *ForwardSigner) sign(method, path, owner, request string) string {
	timestamp := strconv.FormatInt(s.now().UnixMilli(), 10)
	return forwardSignatureScheme + "." + timestamp + "." + s.mac(method, path, owner, request, timestamp)
}

func (s *ForwardSigner) verify(r *http.Request, owner string) error {
	scheme, rest, found := strings.Cut(r.Header.Get(forwardSignatureHeader), ".")
	timestamp, signature, foundSignature := strings.Cut(rest, ".")
	if !found || !foundSignature || scheme != forwardSignatureScheme {
		return errForwardSignature
	}
	millis, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errForwardSignature
	}
	age := s.now().Sub(time.UnixMilli(millis))
	if age > forwardSignatureSkew || age < -forwardSignatureSkew {
		return errForwardSignature
	}
	expected := s.mac(r.Method, r.URL.EscapedPath(), owner, r.Header.Get(forwardRequestHeader), timestamp)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return errForwardSignature
	}
	return nil
}

func (s *ForwardSigner) mac(method, path, owner, request, timestamp string) string {
	digest := sha256.Sum256([]byte(request))
	mac := hmac.New(sha256.New, s.key)
	for _, part := range []string{forwardSignatureScheme, method, path, owner, hex.EncodeToString(digest[:]), timestamp} {
		mac.Write([]byte(part))
		mac.Write([]byte{'\n'})
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
