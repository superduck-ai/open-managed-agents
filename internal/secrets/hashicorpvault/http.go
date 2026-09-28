package hashicorpvault

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
	token, err := readToken(p.tokenFile)
	if err != nil {
		return transitData{}, failure(operation, "TokenUnavailable", 0)
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
	if len(token) == 0 || bytes.ContainsFunc(token, func(r rune) bool { return r < 0x21 || r > 0x7e }) {
		clear(data)
		return nil, failure("token", "TokenUnavailable", 0)
	}
	return token, nil
}
