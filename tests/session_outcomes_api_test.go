package tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestDefineOutcomeFileRubricMissingFileReturns404(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("outcome-file-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"outcome-file-agent"}`)
	env := createEnvironment(t, app, `{"name":"outcome-file-env"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)

	body := `{"events":[{"type":"user.define_outcome","description":"d","rubric":{"type":"file","file_id":"file_missing"}}]}`
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/events?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("file rubric 文件缺失 status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}
