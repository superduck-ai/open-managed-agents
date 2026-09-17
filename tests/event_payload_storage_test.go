package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func TestEventPayloadStorageRoundTrip(t *testing.T) {
	objects := newFakeStore("event-payload-test")
	app := newTestAppWithStore(t, nil, objects)
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"payload-test"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"payload-test"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, codeID)
	content := strings.Repeat("中", 12000) + "payload-end"
	private := `{"type":"assistant","uuid":"private-large","message":{"role":"assistant","content":` + quoteJSON(content) + `}}`
	request := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"payload":` + private + `}]}`
	postCodeSessionWorkerInternalEvents(t, app, codeID, strings.Replace(request, `"events":[{"payload":`+private+`}]`, `"events":[{"payload":`+private+`},{"payload":`+private+`}]`, 1))
	if len(objects.objects) != 1 {
		t.Fatalf("same batch uploaded %d objects", len(objects.objects))
	}
	objects.uploadErr = errors.New("S3 unavailable during replay")
	postCodeSessionWorkerInternalEvents(t, app, codeID, request)
	objects.uploadErr = nil
	if len(objects.objects) != 1 {
		t.Fatal("private replay uploaded duplicate payload")
	}
	page := getCodeSessionWorkerInternalEvents(t, app, codeID, "internal-events")
	if len(page.Data) != 1 {
		t.Fatalf("duplicate private events: %d", len(page.Data))
	}
	assertRawJSONEqual(t, page.Data[0].Payload, private)
	var raw []byte
	if err := app.pool.QueryRow(context.Background(), `select payload from code_session_internal_events where code_session_external_id=$1 and payload_uuid='private-large'`, codeID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	assertStoredPayloadSummary(t, raw, len(private))
	postCodeSessionIngressEvents(t, app, codeID, `{"events":[`+private+`]}`)
	objectCount := len(objects.objects)
	postCodeSessionIngressEvents(t, app, codeID, `{"events":[`+private+`]}`)
	if len(objects.objects) != objectCount {
		t.Fatal("public replay uploaded duplicate payload")
	}

	events := listSessionEvents(t, app, session.ID, "", config.DefaultAPIKey)
	var publicID string
	for _, event := range events.Data {
		if bytes.Contains(event, []byte("payload-end")) {
			var id struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(event, &id); err != nil {
				t.Fatal(err)
			}
			publicID = id.ID
		}
	}
	if publicID == "" {
		t.Fatal("full history omitted externalized event")
	}
	if err := app.pool.QueryRow(context.Background(), `select payload from session_events where session_external_id=$1 and external_id=$2`, session.ID, publicID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var summary eventpayload.Summary
	if err := json.Unmarshal(raw, &summary); err != nil || summary.BlobRef == nil {
		t.Fatalf("public not externalized: %s %v", raw, err)
	}
	// Existing APIs always return full payloads and fail when an object is unavailable.
	saved := objects.objects
	objects.objects = make(map[string]fakeObject)
	resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+session.ID+"/events?beta=true", nil, config.DefaultAPIKey, true)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("missing object returned successful full history")
	}
	objects.objects = saved
	resp = doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+session.ID+"/events?beta=true", nil, config.DefaultAPIKey, true)
	body := readAll(t, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "payload-end") {
		t.Fatalf("full history response %d", resp.StatusCode)
	}

}
func assertStoredPayloadSummary(t *testing.T, payload []byte, size int) {
	t.Helper()
	var summary eventpayload.Summary
	if err := json.Unmarshal(payload, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.BlobRef == nil || summary.Size != int64(size) || len(summary.Preview) > 512 || len(payload) >= 1024 {
		t.Fatalf("invalid summary: %s", payload)
	}
}
