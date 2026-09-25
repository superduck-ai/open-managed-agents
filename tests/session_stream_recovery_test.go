package tests

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPendingInputGetsStreamPositionOnlyAfterAcknowledgement(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("pending-stream-position-bucket"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	scope := db.SessionEventStreamParams{WorkspaceUUID: worker.WorkspaceUUID, SessionExternalID: worker.SessionExternalID, PrimaryOnly: true}
	before, err := app.db.SessionEventStreamPosition(t.Context(), scope, "")
	if err != nil {
		t.Fatal(err)
	}
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":"sevt_tool","result":"allow"}]}`, defaultTestKey)
	eventID := sessionEventStringField(t, sent.Data[0], "id")
	if got := sessionInputProcessedAt(t, sent.Data[0]); got != "" {
		t.Fatalf("pending input processed_at = %q", got)
	}
	if _, err := app.db.SessionEventStreamPosition(t.Context(), scope, eventID); err != db.ErrInvalidCursor {
		t.Fatalf("pending input stream cursor error = %v, want invalid cursor", err)
	}
	if _, changed, err := app.db.MarkSessionEventProcessed(t.Context(), worker, eventID, time.Now().UTC()); err != nil || !changed {
		t.Fatalf("acknowledge pending input = %t, %v", changed, err)
	}
	position, err := app.db.SessionEventStreamPosition(t.Context(), scope, eventID)
	if err != nil || position <= before {
		t.Fatalf("acknowledged position = %d, before = %d, error = %v", position, before, err)
	}
	page, err := app.db.ListSessionStreamEventsPage(t.Context(), scope, before, 100)
	if err != nil || len(page) == 0 || page[len(page)-1].ExternalID != eventID {
		t.Fatalf("acknowledged input missing from stream page: %+v, %v", page, err)
	}
}

func TestSessionStreamReplaysPersistedEventsAfterCursor(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-stream-recovery-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"stream-recovery-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"stream-recovery-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	workerID := launchLocalCodeSession(t, app, session.ID)

	postRecoveryMessage(t, app, workerID, "first")
	initial := listSessionEvents(t, app, session.ID, "order=asc&limit=100&types[]=agent.message", defaultTestKey)
	if len(initial.Data) == 0 {
		t.Fatal("new session has no cursor event")
	}
	cursor := sessionEventStringField(t, initial.Data[len(initial.Data)-1], "id")
	postRecoveryMessage(t, app, workerID, "second")
	postRecoveryMessage(t, app, workerID, "third")

	firstResponse, firstScanner := openRecoveryStream(t, app, session.ID, cursor)
	defer firstResponse.Body.Close()
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("initial replay status = %d", firstResponse.StatusCode)
	}
	second := readRecoveryMessage(t, firstScanner)
	third := readRecoveryMessage(t, firstScanner)
	if !strings.Contains(second.data, "second") || !strings.Contains(third.data, "third") || second.id == "" || third.id == "" || second.id == third.id || !strings.Contains(second.data, `"id":"`+second.id+`"`) || !strings.Contains(third.data, `"id":"`+third.id+`"`) {
		t.Fatalf("replay frames = %+v, %+v", second, third)
	}
	postRecoveryMessage(t, app, workerID, "fourth")
	fourth := readRecoveryMessage(t, firstScanner)
	if !strings.Contains(fourth.data, "fourth") || fourth.id == "" {
		t.Fatalf("live frame = %+v", fourth)
	}
	firstResponse.Body.Close()

	resumeResponse, resumeScanner := openRecoveryStream(t, app, session.ID, second.id)
	defer resumeResponse.Body.Close()
	if resumeResponse.StatusCode != http.StatusOK {
		t.Fatalf("resume status = %d", resumeResponse.StatusCode)
	}
	for _, want := range []sseRecoveryFrame{third, fourth} {
		got := readRecoveryMessage(t, resumeScanner)
		if got.id != want.id || got.data != want.data {
			t.Fatalf("resumed frame = %+v, want %+v", got, want)
		}
	}
	resumeResponse.Body.Close()

	liveResponse, liveScanner := openRecoveryStream(t, app, session.ID, "")
	defer liveResponse.Body.Close()
	if liveResponse.StatusCode != http.StatusOK {
		t.Fatalf("live status = %d", liveResponse.StatusCode)
	}
	postRecoveryMessage(t, app, workerID, "fifth")
	live := readRecoveryMessage(t, liveScanner)
	if !strings.Contains(live.data, "fifth") || live.id == "" {
		t.Fatalf("no-cursor live frame = %+v", live)
	}

	invalidResponse, _ := openRecoveryStream(t, app, session.ID, "sevt_unknown")
	defer invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown cursor status = %d, want 400", invalidResponse.StatusCode)
	}
}

func TestThreadStreamCursorIsScopedToThread(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("thread-stream-recovery-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"thread-stream-recovery-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"thread-stream-recovery-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	workerID := launchLocalCodeSession(t, app, session.ID)
	threadID := "sthr_" + strings.TrimPrefix(session.ID, "sesn_")
	postCodeSessionIngressEvents(t, app, workerID, `{"events":[{"type":"session.thread_created","uuid":"thread-recovery-`+threadID+`","session_thread_id":`+quoteJSON(threadID)+`,"agent_name":"analyst","created_at":"2026-06-16T01:00:00Z"}]}`)
	postThreadRecoveryMessage(t, app, workerID, threadID, "child first")
	child := listThreadEvents(t, app, session.ID, threadID, defaultTestKey)
	var childCursor string
	for _, event := range child.Data {
		if strings.Contains(string(event), "child first") {
			childCursor = sessionEventStringField(t, event, "id")
		}
	}
	if childCursor == "" {
		t.Fatalf("child event missing from thread history: %+v", child.Data)
	}
	postThreadRecoveryMessage(t, app, workerID, threadID, "child second")
	threadResponse, scanner := openRecoveryStreamPath(t, app, "/v1/sessions/"+session.ID+"/threads/"+threadID+"/stream?beta=true", childCursor)
	defer threadResponse.Body.Close()
	if threadResponse.StatusCode != http.StatusOK {
		t.Fatalf("thread replay status = %d", threadResponse.StatusCode)
	}
	if got := readRecoveryMessage(t, scanner); !strings.Contains(got.data, "child second") || got.id == "" {
		t.Fatalf("thread replay frame = %+v", got)
	}

	primaryResponse, _ := openRecoveryStream(t, app, session.ID, childCursor)
	defer primaryResponse.Body.Close()
	if primaryResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("child cursor in primary stream status = %d, want 400", primaryResponse.StatusCode)
	}
	primaryHistory := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
	primaryCursor := sessionEventStringField(t, primaryHistory.Data[0], "id")
	wrongThreadResponse, _ := openRecoveryStreamPath(t, app, "/v1/sessions/"+session.ID+"/threads/"+threadID+"/stream?beta=true", primaryCursor)
	defer wrongThreadResponse.Body.Close()
	if wrongThreadResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("primary cursor in child stream status = %d, want 400", wrongThreadResponse.StatusCode)
	}
}

type sseRecoveryFrame struct {
	id   string
	data string
}

func openRecoveryStream(t *testing.T, app *testApp, sessionID, cursor string) (*http.Response, *bufio.Scanner) {
	return openRecoveryStreamPath(t, app, "/v1/sessions/"+sessionID+"/events/stream?beta=true", cursor)
}

func openRecoveryStreamPath(t *testing.T, app *testApp, path, cursor string) (*http.Response, *bufio.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", defaultTestKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	req.Header.Set("Accept", "text/event-stream")
	if cursor != "" {
		req.Header.Set("Last-Event-ID", cursor)
	}
	response, err := app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response, bufio.NewScanner(response.Body)
}

func readRecoveryMessage(t *testing.T, scanner *bufio.Scanner) sseRecoveryFrame {
	t.Helper()
	for {
		frame := sseRecoveryFrame{}
		readLine := false
		for scanner.Scan() {
			readLine = true
			line := scanner.Text()
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "id: ") {
				frame.id = strings.TrimPrefix(line, "id: ")
			}
			if strings.HasPrefix(line, "data: ") {
				frame.data = strings.TrimPrefix(line, "data: ")
			}
		}
		if strings.Contains(frame.data, `"type":"agent.message"`) {
			return frame
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("read stream: %v", err)
		}
		if !readLine {
			t.Fatal("stream closed before agent.message")
		}
	}
}

func postRecoveryMessage(t *testing.T, app *testApp, workerID, text string) {
	t.Helper()
	postCodeSessionIngressEvents(t, app, workerID, `{"events":[{"type":"assistant","uuid":"assistant-recovery-`+workerID+`-`+text+`","message":{"role":"assistant","content":"`+text+`"},"created_at":"2026-06-16T01:10:00Z"}]}`)
}

func postThreadRecoveryMessage(t *testing.T, app *testApp, workerID, threadID, message string) {
	t.Helper()
	postCodeSessionIngressEvents(t, app, workerID, `{"events":[{"type":"agent.message","uuid":"child-recovery-`+workerID+`-`+message+`","_owner_session_thread_id":`+quoteJSON(threadID)+`,"content":[{"type":"text","text":`+quoteJSON(message)+`}],"created_at":"2026-06-16T01:10:00Z"}]}`)
}
