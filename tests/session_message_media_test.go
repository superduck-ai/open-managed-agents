//go:build integration || e2e || e2b_integration

package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	testPNGBase64 = "iVBORw0KGgo="
	testPDFBase64 = "JVBERi0xLjQK"
)

type messageMediaFixture struct {
	app           *testApp
	agentID       string
	environmentID string
	sessionID     string
	codeSessionID string
}

func newMessageMediaFixture(t *testing.T, name string) messageMediaFixture {
	t.Helper()
	app := newTestAppWithStore(t, nil, newFakeStore(name+"-bucket"))
	t.Cleanup(app.close)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"`+name+`-agent"}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	env := createEnvironment(t, app, `{"name":"`+name+`-env"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, env.ID) })
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	t.Cleanup(func() { cleanupSession(t, app, session.ID) })
	return messageMediaFixture{
		app: app, agentID: agent.ID, environmentID: env.ID,
		sessionID: session.ID, codeSessionID: launchLocalCodeSession(t, app, session.ID),
	}
}

func (f messageMediaFixture) sendStatus(t *testing.T, body string) (int, string) {
	t.Helper()
	resp := doSessionRequest(t, f.app, http.MethodPost, "/v1/sessions/"+f.sessionID+"/events?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	return resp.StatusCode, string(readAll(t, resp.Body))
}

func (f messageMediaFixture) queuedUserContent(t *testing.T) []map[string]any {
	t.Helper()
	inbound, err := listQueuedCodeSessionInboundEvents(f.app, f.codeSessionID)
	if err != nil {
		t.Fatalf("list inbound: %v", err)
	}
	for i := len(inbound) - 1; i >= 0; i-- {
		var payload struct {
			Type    string `json:"type"`
			Message struct {
				Content []map[string]any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(inbound[i].Payload, &payload) == nil && payload.Type == "user" {
			return payload.Message.Content
		}
	}
	t.Fatal("no queued user payload")
	return nil
}

func TestSessionMessageMediaRejectsInvalidInput(t *testing.T) {
	fixture := newMessageMediaFixture(t, "message-media-invalid")
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "invalid base64", content: `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"%%%"}}`, want: "valid base64"},
		{name: "unsupported media type", content: `{"type":"image","source":{"type":"base64","media_type":"image/bmp","data":"AA=="}}`, want: "not supported"},
		{name: "relative url", content: `{"type":"image","source":{"type":"url","url":"not a url"}}`, want: "absolute http or https URL"},
		{name: "non http url", content: `{"type":"document","source":{"type":"url","url":"file:///etc/passwd"}}`, want: "absolute http or https URL"},
		{name: "mismatched image bytes", content: `{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"` + testPNGBase64 + `"}}`, want: "does not match media_type"},
		{name: "unknown file", content: `{"type":"document","source":{"type":"file","file_id":"file_missing"}}`, want: "was not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, body := fixture.sendStatus(t, `{"events":[{"type":"user.message","content":[`+test.content+`]}]}`)
			if status != http.StatusBadRequest || !strings.Contains(body, test.want) {
				t.Fatalf("send status = %d body = %s, want 400 containing %q", status, body, test.want)
			}
		})
	}
	if inbound, err := listQueuedCodeSessionInboundEvents(fixture.app, fixture.codeSessionID); err != nil || countUserPayloads(inbound) != 0 {
		t.Fatalf("rejected media queued worker input: %d, %v", countUserPayloads(inbound), err)
	}
}

func countUserPayloads(events []db.CodeSessionEvent) int {
	count := 0
	for _, event := range events {
		if bytes.Contains(event.Payload, []byte(`"type":"user"`)) {
			count++
		}
	}
	return count
}

func TestSessionMessageMediaStoresAttachmentsAsSessionFileReferences(t *testing.T) {
	fixture := newMessageMediaFixture(t, "message-media-refs")
	app := fixture.app
	uploaded := uploadFile(t, app, "notes.txt", "text/plain", []byte("uploaded notes"))
	t.Cleanup(func() { deleteFile(t, app, uploaded.ID) })
	body := `{"events":[{"type":"user.message","content":[
		{"type":"text","text":"Review these"},
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + testPNGBase64 + `"}},
		{"type":"image","source":{"type":"url","url":"https://example.com/diagram.png"}},
		{"type":"document","title":"Spec","context":"draft","source":{"type":"base64","media_type":"application/pdf","data":"` + testPDFBase64 + `"}},
		{"type":"document","source":{"type":"text","media_type":"text/plain","data":"inline notes"}},
		{"type":"document","source":{"type":"file","file_id":` + quoteJSON(uploaded.ID) + `}}
	]}]}`
	sent := sendSessionEvents(t, app, fixture.sessionID, body, defaultTestKey)
	if len(sent.Data) == 0 || !bytes.Contains(sent.Data[len(sent.Data)-1], []byte(testPNGBase64)) {
		t.Fatalf("send response does not echo original content: %s", sent.Data)
	}

	content := fixture.queuedUserContent(t)
	raw, _ := json.Marshal(content)
	if bytes.Contains(raw, []byte(testPNGBase64)) || bytes.Contains(raw, []byte(testPDFBase64)) || bytes.Contains(raw, []byte("inline notes")) {
		t.Fatalf("worker content carries inline attachment bytes: %s", raw)
	}
	if len(content) != 6 || content[0]["text"] != "Review these" {
		t.Fatalf("worker content = %s", raw)
	}
	if source := content[2]["source"].(map[string]any); source["type"] != "url" || source["url"] != "https://example.com/diagram.png" {
		t.Fatalf("url image source = %v", source)
	}
	if content[3]["title"] != "Spec" || content[3]["context"] != "draft" {
		t.Fatalf("document fields were not preserved: %v", content[3])
	}
	ctx := context.Background()
	ids := getDefaultDBIDs(t, app.pool)
	session, found, err := app.db.GetSession(ctx, ids.WorkspaceUUID, fixture.sessionID)
	if err != nil || !found {
		t.Fatalf("load session = (%t, %v)", found, err)
	}
	wantMedia := map[int]string{1: "image/png", 3: "application/pdf", 4: "text/plain", 5: "text/plain"}
	wantBytes := map[int]string{4: "inline notes", 5: "uploaded notes"}
	for index, mediaType := range wantMedia {
		source := content[index]["source"].(map[string]any)
		fileID, _ := source["file_id"].(string)
		if source["type"] != "file" || !strings.HasPrefix(fileID, "file_") || fileID == uploaded.ID {
			t.Fatalf("block %d source = %v, want session file reference", index, source)
		}
		file, err := app.db.GetSessionAttachmentFile(ctx, ids.WorkspaceUUID, session.UUID, fileID)
		if err != nil || file.MimeType != mediaType {
			t.Fatalf("attachment %d = %+v, %v; want %s", index, file, err, mediaType)
		}
		if want, ok := wantBytes[index]; ok {
			object, err := app.store.Open(ctx, file.S3Key, nil)
			if err != nil {
				t.Fatalf("open attachment %d: %v", index, err)
			}
			data, _ := io.ReadAll(object.Body)
			object.Body.Close()
			if string(data) != want {
				t.Fatalf("attachment %d bytes = %q, want %q", index, data, want)
			}
		}
		if _, err := app.db.GetFile(ctx, ids.WorkspaceUUID, fileID); err == nil {
			t.Fatalf("attachment %d is visible through the Files API", index)
		}
	}

	history := listSessionEvents(t, app, fixture.sessionID, "", defaultTestKey)
	echoed := false
	for _, event := range history.Data {
		echoed = echoed || (bytes.Contains(event, []byte(testPNGBase64)) && bytes.Contains(event, []byte(uploaded.ID)))
	}
	if !echoed {
		t.Fatal("public history does not echo the original user.message content")
	}
}

func grantMessagesToken(t *testing.T, app *testApp, codeSessionID string) string {
	t.Helper()
	token := "sk-ant-oat01-media-" + strings.TrimPrefix(codeSessionID, "cse_")
	if _, err := app.pool.Exec(context.Background(), `
		update code_sessions
		set oauth_access_token_hash = $2, status = 'active', worker_lease_expires_at = now() + interval '10 minutes'
		where external_id = $1
	`, codeSessionID, auth.HashAPIKey(token)); err != nil {
		t.Fatalf("grant Messages token: %v", err)
	}
	return token
}

func TestMessagesProxyExpandsOnlyOwnSessionAttachments(t *testing.T) {
	upstreamBodies := make(chan []byte, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamBodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[]}`)
	}))
	defer upstream.Close()

	fixture := newMessageMediaFixture(t, "message-media-proxy")
	app := fixture.app
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Messages media", upstream.URL, "server-provider-key", messagesTestModel)
	sendSessionEvents(t, app, fixture.sessionID, `{"events":[{"type":"user.message","content":[
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+testPNGBase64+`"}},
		{"type":"document","source":{"type":"text","media_type":"text/plain","data":"proxy notes"}}
	]}]}`, defaultTestKey)
	content := fixture.queuedUserContent(t)
	imageID := content[0]["source"].(map[string]any)["file_id"].(string)
	documentID := content[1]["source"].(map[string]any)["file_id"].(string)
	request := `{"model":"` + messagesTestModel + `","max_tokens":16,"messages":[{"role":"user","content":[
		{"type":"image","source":{"type":"file","file_id":` + quoteJSON(imageID) + `}},
		{"type":"document","source":{"type":"file","file_id":` + quoteJSON(documentID) + `}}
	]}]}`

	other := createSession(t, app, `{"agent":`+quoteJSON(fixture.agentID)+`,"environment_id":`+quoteJSON(fixture.environmentID)+`}`)
	t.Cleanup(func() { cleanupSession(t, app, other.ID) })
	otherToken := grantMessagesToken(t, app, launchLocalCodeSession(t, app, other.ID))
	response := doMessagesRequest(t, app, otherToken, request)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("other session Messages status = %d", response.StatusCode)
	}
	select {
	case leaked := <-upstreamBodies:
		t.Fatalf("another session reached upstream with foreign attachments: %s", leaked)
	default:
	}

	response = doMessagesRequest(t, app, grantMessagesToken(t, app, fixture.codeSessionID), request)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("own session Messages status = %d", response.StatusCode)
	}
	expanded := <-upstreamBodies
	if !bytes.Contains(expanded, []byte(`"data":"`+testPNGBase64+`"`)) || !bytes.Contains(expanded, []byte(`proxy notes`)) || bytes.Contains(expanded, []byte("file_id")) {
		t.Fatalf("upstream body was not expanded: %s", expanded)
	}
}

func TestSessionMessageMediaMaterializesOffloadedPayloads(t *testing.T) {
	fixture := newMessageMediaFixture(t, "message-media-offload")
	signature, _ := base64.StdEncoding.DecodeString(testPNGBase64)
	large := base64.StdEncoding.EncodeToString(append(signature, bytes.Repeat([]byte{0}, 64*1024-len(signature))...))
	sendSessionEvents(t, fixture.app, fixture.sessionID, `{"events":[{"type":"user.message","content":[
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+large+`"}}
	]}]}`, defaultTestKey)
	content := fixture.queuedUserContent(t)
	raw, _ := json.Marshal(content)
	source := content[0]["source"].(map[string]any)
	if bytes.Contains(raw, []byte(large[:64])) || source["type"] != "file" {
		t.Fatalf("offloaded payload was not materialized: %.200s", raw)
	}
	ctx := context.Background()
	ids := getDefaultDBIDs(t, fixture.app.pool)
	session, found, err := fixture.app.db.GetSession(ctx, ids.WorkspaceUUID, fixture.sessionID)
	if err != nil || !found {
		t.Fatalf("load session = (%t, %v)", found, err)
	}
	file, err := fixture.app.db.GetSessionAttachmentFile(ctx, ids.WorkspaceUUID, session.UUID, source["file_id"].(string))
	if err != nil || file.SizeBytes != 64*1024 {
		t.Fatalf("attachment = %+v, %v; want 65536 bytes", file, err)
	}
}

func TestSessionMessageMediaReplayReusesStoredAttachmentAfterSourceDeletion(t *testing.T) {
	fixture := newMessageMediaFixture(t, "message-media-replay")
	app := fixture.app
	uploaded := uploadFile(t, app, "spec.pdf", "application/pdf", []byte("%PDF-1.4\nreplay"))
	sent := sendSessionEvents(t, app, fixture.sessionID, `{"events":[{"type":"user.message","content":[
		{"type":"document","title":"Spec","source":{"type":"file","file_id":`+quoteJSON(uploaded.ID)+`}}
	]}]}`, defaultTestKey)
	var message struct {
		ID string `json:"id"`
	}
	if len(sent.Data) == 0 || json.Unmarshal(sent.Data[len(sent.Data)-1], &message) != nil || message.ID == "" {
		t.Fatalf("send response = %s", sent.Data)
	}
	first := fixture.queuedUserContent(t)[0]["source"].(map[string]any)["file_id"]
	deleteFile(t, app, uploaded.ID)

	ctx := context.Background()
	ids := getDefaultDBIDs(t, app.pool)
	session, found, err := app.db.GetSession(ctx, ids.WorkspaceUUID, fixture.sessionID)
	if err != nil || !found {
		t.Fatalf("load session = (%t, %v)", found, err)
	}
	event, err := app.db.GetSessionEvent(ctx, ids.WorkspaceUUID, fixture.sessionID, message.ID)
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	if err := newCodeSessionService(app, nil, nil).QueuePublicSessionEvents(ctx, session, []db.SessionEvent{event}); err != nil {
		t.Fatalf("replay event: %v", err)
	}
	replayed := fixture.queuedUserContent(t)[0]
	if source := replayed["source"].(map[string]any); source["type"] != "file" || source["file_id"] != first || replayed["title"] != "Spec" {
		t.Fatalf("replayed block = %v, want stored attachment %v", replayed, first)
	}
}
