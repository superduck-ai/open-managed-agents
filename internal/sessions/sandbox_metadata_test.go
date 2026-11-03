package sessions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvironmentWorkMetadataCopiesTimeoutAndEnv(t *testing.T) {
	if string(environmentWorkMetadata(nil)) != `{}` || string(environmentWorkMetadata([]byte(`{"experiment_id":"evalexp_test"}`))) != `{}` {
		t.Fatal("session metadata without timeout or env changed work metadata")
	}
	raw := environmentWorkMetadata([]byte(`{"timeout_seconds":"60","env":"{\"TRACE\":\"1\"}","experiment_id":"evalexp_test"}`))
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["timeout_seconds"] != float64(60) {
		t.Fatalf("timeout = %#v", body["timeout_seconds"])
	}
	env, _ := body["env"].(map[string]any)
	if env["TRACE"] != "1" || strings.Contains(string(raw), "experiment_id") {
		t.Fatalf("work metadata = %s", raw)
	}
}
