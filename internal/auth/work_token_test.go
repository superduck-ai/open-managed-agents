package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestWorkCredentialMaterial(t *testing.T) {
	scope := WorkTokenScope{"11111111-1111-1111-1111-111111111111", "env_owned", "work_owned"}
	secret, err := NewWorkSecret(scope)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Token string `json:"sessions_token"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		t.Fatal("not SDK secret JSON")
	}
	got, ok := ParseWorkToken(payload.Token)
	if !ok || got != scope || !MatchesWorkSecret(payload.Token, &secret) {
		t.Fatal("round trip failed")
	}
	next, _ := NewWorkSecret(scope)
	if next == secret || MatchesWorkSecret(payload.Token+"x", &secret) || MatchesWorkSecret(payload.Token, nil) {
		t.Fatal("credential identity failed")
	}
	for _, malformed := range []string{"", "oma_work", payload.Token + ".extra", "oma_work.bad.env_a.work_b.invalid"} {
		if _, ok := ParseWorkToken(malformed); ok {
			t.Fatal("accepted malformed token")
		}
	}
}
