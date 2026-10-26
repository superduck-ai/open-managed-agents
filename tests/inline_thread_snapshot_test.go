//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestInlineThreadSnapshotIsPublicAndStable(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("inline-thread-snapshot"))
	worker, epoch := newPayloadIntegrationSessionWithAgent(t, app, `{"model":"claude-opus-4-6","name":"parent","system":"parent prompt"}`)
	child := "sthr_inline_" + worker.SessionExternalID
	input := internalPayloadRequest(epoch, `{"type":"session.thread_created","uuid":"inline-created","session_thread_id":`+quoteJSON(child)+`,"agent_name":"inline-worker","agent":{"type":"inline","name":"inline-worker","system":"child prompt"}}`)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, input)
	read := func() map[string]any {
		response := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+worker.SessionExternalID+"/threads/"+child+"?beta=true", nil, defaultTestKey, true)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("get thread: %d %s", response.StatusCode, response.Body)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Agent map[string]any `json:"agent"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		return body.Agent
	}
	agent := read()
	if agent["type"] != "inline" || agent["name"] != "inline-worker" || agent["system"] != "child prompt" || agent["model"] == nil {
		t.Fatalf("invalid inline snapshot: %v", agent)
	}
	for _, key := range []string{"id", "version", "multiagent"} {
		if _, ok := agent[key]; ok {
			t.Fatalf("resource identity %s in inline", key)
		}
	}
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, input)
	after := read()
	beforeRaw, _ := json.Marshal(agent)
	afterRaw, _ := json.Marshal(after)
	if string(beforeRaw) != string(afterRaw) {
		t.Fatal("replay changed snapshot")
	}
}
