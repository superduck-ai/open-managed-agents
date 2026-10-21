package vaults

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestVaultCreateInvalidMetadataRejectedBeforePersistence(t *testing.T) {
	for _, value := range []string{`null`, `{"n":3}`, `{"n":null}`, `[]`, `"text"`} {
		t.Run(value, func(t *testing.T) {
			h := NewHandler(config.Config{}, nil, nil, nil, nil)
			r := httptest.NewRequest(http.MethodPost, "/?beta=true", strings.NewReader(`{"display_name":"metadata contract","metadata":`+value+`}`))
			r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{CredentialType: auth.CredentialTypeAPIKey}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"invalid_request_error"`) {
				t.Fatalf("invalid metadata response status=%d", w.Code)
			}
		})
	}
}

func TestVaultMetadataNoOpPatchPreservesSeedWithoutChangingCredentialReset(t *testing.T) {
	seed := json.RawMessage(`{"keep":"original","run":"owned"}`)
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`null`)} {
		got, err := patchVaultMetadata(seed, raw)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]string
		if err := json.Unmarshal(got, &value); err != nil || len(value) != 2 || value["keep"] != "original" || value["run"] != "owned" {
			t.Fatal("Vault no-op patch changed existing metadata")
		}
	}
	if _, err := patchVaultMetadata(seed, json.RawMessage(`{"n":3}`)); err == nil {
		t.Fatal("numeric patch accepted")
	}
	cleared, err := patchMetadata(seed, json.RawMessage(`null`))
	if err != nil || string(cleared) != "{}" {
		t.Fatal("Credential reset changed", err)
	}
	patched, err := patchVaultMetadata(seed, json.RawMessage(`{"keep":null}`))
	var value map[string]string
	if err != nil || json.Unmarshal(patched, &value) != nil || len(value) != 1 || value["run"] != "owned" {
		t.Fatal("per-key deletion changed")
	}
}

func TestVaultCreateMetadataOmissionAndStringObjectRemainValid(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"keep":"original"}`)} {
		got, err := normalizeVaultCreateMetadata(raw)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]string
		if err := json.Unmarshal(got, &value); err != nil || value == nil {
			t.Fatal("creation metadata must remain an object")
		}
		if len(raw) > 2 && value["keep"] != "original" {
			t.Fatal("string metadata lost")
		}
	}
}
