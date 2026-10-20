package agents

import (
	"encoding/json"
	"errors"
	"testing"
)

func identityModelID(modelID string) (string, error) {
	return modelID, nil
}

func TestSessionModelOverrideRejectsUnresolvedModel(t *testing.T) {
	unresolved := errors.New("unresolved")
	_, err := NormalizeSessionModelOverride(json.RawMessage(`{"id":"fixture"}`), func(string) (string, error) {
		return "", unresolved
	})
	if !errors.Is(err, unresolved) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionModelOverrideReplacesBaseAndKeepsGeo(t *testing.T) {
	raw, err := NormalizeSessionModelOverride(json.RawMessage(`{"id":"fixture","inference_geo":"us"}`), identityModelID)
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]any
	if err := json.Unmarshal(raw, &model); err != nil {
		t.Fatal(err)
	}
	if model["id"] != "fixture" || model["inference_geo"] != "us" || model["speed"] != "standard" {
		t.Fatal(string(raw))
	}
	for _, bad := range []string{`null`, `{}`, `{"id":"fixture","inference_geo":"eu"}`} {
		if _, err := NormalizeSessionModelOverride(json.RawMessage(bad), identityModelID); err == nil {
			t.Fatal("bad model override accepted", bad)
		}
	}
}

func TestSessionModelOverrideStoresCanonicalModelID(t *testing.T) {
	raw, err := NormalizeSessionModelOverride(json.RawMessage(`"kimi-k2.5"`), func(modelID string) (string, error) {
		return "default/" + modelID, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var model struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &model); err != nil || model.ID != "default/kimi-k2.5" {
		t.Fatalf("model = %s, err = %v", raw, err)
	}
}
