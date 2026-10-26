package vaults

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func newValidationCapture(client *http.Client, auth *mcpOAuthCredentialAuth, secret mcpOAuthCredentialSecret, clients []config.PlatformOAuthClientConfig) *validationCapture {
	v := &validationCapture{base: client.Transport, values: []string{secret.AccessToken}}
	if v.base == nil {
		v.base = http.DefaultTransport
	}
	if secret.Refresh != nil {
		v.values = append(v.values, secret.Refresh.RefreshToken)
		clientSecret, _ := resolveMCPOAuthRefreshClientSecret(auth, secret, clients)
		v.values = append(v.values, clientSecret)
		if auth.Refresh != nil && clientSecret != "" {
			v.values = append(v.values, auth.Refresh.ClientID+":"+clientSecret,
				url.QueryEscape(auth.Refresh.ClientID)+":"+url.QueryEscape(clientSecret))
		}
	}
	copyClient := *client
	copyClient.Transport = v
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	v.client = &copyClient
	return v
}

func validationSafeResponse(resp *http.Response, body []byte, values []string) *validationHTTPResponse {
	const maxBody = 8192
	result := &validationHTTPResponse{StatusCode: resp.StatusCode, ContentType: resp.Header.Get("Content-Type")}
	if len(body) > 1<<20 {
		result.Body, result.BodyTruncated = "[response body omitted]", true
		return result
	}
	issued, valid := issuedOAuthResponseSecrets(body)
	if !valid {
		trimmed := bytes.TrimSpace(body)
		if bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("[")) || strings.HasPrefix(result.ContentType, "application/json") {
			if len(body) != 0 {
				result.Body, result.BodyTruncated = "[response body omitted]", true
				return result
			}
		}
	}
	values = append(append([]string(nil), values...), issued...)
	variants := make([]string, 0, len(values)*8)
	for _, value := range values {
		if value == "" {
			continue
		}
		encoded, _ := json.Marshal(value)
		variants = append(variants, value, string(encoded[1:len(encoded)-1]), html.EscapeString(value),
			url.QueryEscape(value), url.PathEscape(value), base64.StdEncoding.EncodeToString([]byte(value)),
			base64.RawStdEncoding.EncodeToString([]byte(value)), base64.URLEncoding.EncodeToString([]byte(value)),
			base64.RawURLEncoding.EncodeToString([]byte(value)), hex.EncodeToString([]byte(value)), strings.ToUpper(hex.EncodeToString([]byte(value))))
	}
	sort.Slice(variants, func(i, j int) bool { return len(variants[i]) > len(variants[j]) })
	cleaned := string(body)
	for _, variant := range variants {
		cleaned = strings.ReplaceAll(cleaned, variant, "[REDACTED]")
	}
	result.BodyTruncated = len(cleaned) > maxBody
	if result.BodyTruncated {
		cleaned = cleaned[:maxBody]
	}
	result.Body = cleaned
	return result
}

func issuedOAuthResponseSecrets(body []byte) ([]string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	var values []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil, false
		}
		if key == "access_token" || key == "refresh_token" {
			var value string
			if json.Unmarshal(raw, &value) == nil {
				values = append(values, value)
			}
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	var extra json.RawMessage
	if decoder.Decode(&extra) != io.EOF {
		return nil, false
	}
	return values, true
}
