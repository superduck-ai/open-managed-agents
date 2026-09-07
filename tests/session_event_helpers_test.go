package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func newSessionEventTestApp(t *testing.T, storeName, agentJSON, environmentJSON string) (*testApp, agentAPIResponse, environmentAPIResponse) {
	t.Helper()
	app := newTestAppWithStore(t, nil, newFakeStore(storeName))
	t.Cleanup(app.close)
	agent := createAgent(t, app, agentJSON)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	env := createEnvironment(t, app, environmentJSON)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, env.ID) })
	return app, agent, env
}

func doSessionEventIngressRequest(t *testing.T, app *testApp, method, codeSessionID, suffix, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, app.baseURL+"/v2/session_ingress/session/"+codeSessionID+suffix, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionIngressToken(t, app, codeSessionID))
	req.Header.Set("Content-Type", "application/json")
	response, err := app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func openSessionEventStream(t *testing.T, app *testApp, ctx context.Context, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", defaultTestKey)
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	stream, err := app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if stream.StatusCode != http.StatusOK {
		stream.Body.Close()
		t.Fatalf("stream status = %d", stream.StatusCode)
	}
	return stream
}

func assertNextSessionFrameType(t *testing.T, scanner *bufio.Scanner, want string) json.RawMessage {
	t.Helper()
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		raw := json.RawMessage(data)
		if got := sessionEventStringField(t, raw, "type"); got != want {
			t.Fatalf("next SSE frame = %s, want %s: %s", got, want, raw)
		}
		return raw
	}
	t.Fatalf("stream ended waiting for %s: %v", want, scanner.Err())
	return nil
}

func assertSessionEventJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var actual, expected any
	for i, raw := range []json.RawMessage{got, want} {
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		target := &actual
		if i == 1 {
			target = &expected
		}
		if err := decoder.Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("JSON mismatch: got %s, want %s", got, want)
	}
}
