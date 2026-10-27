package sessions

import (
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestDuplicateVaultIDsRejectedBeforeDatabaseLookup(t *testing.T) {
	defer func() {
		if recover() != nil {
			t.Fatal("duplicate input reached database lookup instead of validation")
		}
	}()
	h := &Handler{}
	_, err := h.normalizeVaultIDs(httptest.NewRequest("POST", "/sessions", nil), auth.Principal{}, json.RawMessage(`["vlt_duplicate","vlt_duplicate"]`))
	if err == nil || !strings.Contains(err.Error(), "Duplicate vault_id") {
		t.Fatal("duplicate Vault IDs did not produce an explicit validation error")
	}
}

func TestSessionVaultIDInputValidation(t *testing.T) {
	for _, raw := range []string{`["vlt_a","vlt_b","vlt_a"]`, `[""]`, `["  "]`, `[1]`, `{"id":"vlt_a"}`} {
		if _, err := parseSessionVaultIDs(json.RawMessage(raw)); err == nil {
			t.Errorf("invalid input accepted: %s", raw)
		}
	}
	for _, raw := range []string{`null`, `[]`, `["vlt_a"]`, `["vlt_b","vlt_a"]`} {
		ids, err := parseSessionVaultIDs(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if raw == `["vlt_b","vlt_a"]` && (len(ids) != 2 || ids[0] != "vlt_b" || ids[1] != "vlt_a") {
			t.Fatal("distinct Vault order changed")
		}
	}
}

func TestSessionVaultErrorClassification(t *testing.T) {
	private := errors.New("private database failure")
	for _, c := range []struct {
		name    string
		input   error
		kind    apperr.Kind
		message string
	}{
		{"missing", vaultNotFound("vlt_missing", db.ErrNotFound), apperr.NotFound, "Vault 'vlt_missing' was not found."},
		{"duplicate", errors.New("Duplicate vault_id 'vlt_a'."), apperr.InvalidArgument, "Duplicate vault_id 'vlt_a'."},
		{"database failure", internalError("Could not validate Vault binding", private), apperr.Internal, "Could not validate Vault binding"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got *apperr.Error
			if !errors.As(mapVaultBindingError(c.input), &got) || got.Kind != c.kind || got.PublicMessage != c.message {
				t.Fatal("Vault error classification or public message changed")
			}
		})
	}
}
