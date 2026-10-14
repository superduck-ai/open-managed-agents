package hashicorpvault

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
)

type transitRequest struct {
	Plaintext      string `json:"plaintext,omitempty"`
	Ciphertext     string `json:"ciphertext,omitempty"`
	AssociatedData string `json:"associated_data"`
}

type transitData struct {
	Plaintext  string `json:"plaintext"`
	Ciphertext string `json:"ciphertext"`
}

func (p *Provider) call(ctx context.Context, operation string, input transitRequest) (transitData, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return transitData{}, err
	}
	token := []byte(p.token)
	if p.tokenFile != "" {
		var err error
		token, err = readToken(p.tokenFile)
		if err != nil {
			return transitData{}, failure(operation, "TokenUnavailable", 0)
		}
	}
	defer clear(token)
	input.AssociatedData = base64.StdEncoding.EncodeToString([]byte("oma-dek-v1"))
	body, err := json.Marshal(input)
	if err != nil {
		return transitData{}, failure(operation, "InvalidRequest", 0)
	}
	defer clear(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/"+operation+"/"+p.keyName, bytes.NewReader(body))
	if err != nil {
		return transitData{}, failure(operation, "InvalidRequest", 0)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", string(token))
	req.Header.Set("X-Vault-Request", "true")
	resp, err := p.client.Do(req)
	if err != nil {
		return transitData{}, transportError(ctx, operation, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Do not retain or log Vault/APISIX error bodies: they may echo secrets.
		return transitData{}, failure(operation, "HTTPError", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	defer clear(payload)
	if err != nil {
		return transitData{}, transportError(ctx, operation, err)
	}
	if len(payload) > maxResponseBytes {
		return transitData{}, failure(operation, "InvalidResponse", resp.StatusCode)
	}
	var envelope struct {
		Data *transitData `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Data == nil {
		return transitData{}, failure(operation, "InvalidResponse", resp.StatusCode)
	}
	return *envelope.Data, nil
}

// Reopen on every operation so atomic replacement (including Kubernetes
// projected-volume symlinks) takes effect without changing shared client state.
func readToken(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, failure("token", "TokenUnavailable", 0)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		clear(data)
		return nil, failure("token", "TokenUnavailable", 0)
	}
	token := bytes.TrimSpace(data)
	if !validToken(token) {
		clear(data)
		return nil, failure("token", "TokenUnavailable", 0)
	}
	return token, nil
}

func validToken(token []byte) bool {
	return len(token) > 0 && len(token) <= maxResponseBytes && !bytes.ContainsFunc(token, func(r rune) bool { return r < 0x21 || r > 0x7e })
}

func clientTLSConfig(path string) (*tls.Config, error) {
	if path == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("hashicorp_vault: cannot read ca_file")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("hashicorp_vault: cannot load system CA certificates")
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("hashicorp_vault: ca_file contains no valid certificates")
	}
	return &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
}
