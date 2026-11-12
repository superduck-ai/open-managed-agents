//go:build integration

package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"uuid"
)

type nulPublicationObjects struct {
	storage.ObjectStore
	mu         sync.Mutex
	data       map[string][]byte
	uploads    int
	failUpload bool
}

func (s *nulPublicationObjects) Name() string { return "nul-publication-test" }
func (s *nulPublicationObjects) Upload(_ context.Context, key string, body io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads++
	if s.failUpload {
		return storage.UploadResult{}, errors.New("controlled upload failure")
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return storage.UploadResult{}, err
	}
	s.data[key] = raw
	return storage.UploadResult{Size: int64(len(raw))}, nil
}
func (s *nulPublicationObjects) Open(_ context.Context, key string, _ *storage.ByteRange) (storage.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.data[key]
	if !ok {
		return storage.Object{}, errors.New("controlled missing object")
	}
	return storage.Object{Body: io.NopCloser(bytes.NewReader(bytes.Clone(raw))), Size: int64(len(raw))}, nil
}

func TestDSRShortNULPublicResultPostgres(t *testing.T) {
	database := newReconnectPostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	org, workspace, sid, sessionUUID := uuid.NewV4().String(), uuid.NewV4().String(), "sesn_nul_publication", uuid.NewV4().String()
	env, agent, key := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	if _, err := database.ListenerPool().Exec(ctx, `INSERT INTO sessions (uuid,external_id,organization_uuid,workspace_uuid,created_by_api_key_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,status) VALUES ($1,$2,$3,$4,$5,$6,'env_nul',$7,'agent_nul',1,'{}','running')`, sessionUUID, sid, org, workspace, key, env, agent); err != nil {
		t.Fatal(err)
	}
	thread, err := database.CreateSessionThreadIfAbsent(ctx, db.SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_nul", OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: sessionUUID, SessionExternalID: sid, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	code, err := database.CreateCodeSession(ctx, db.CreateCodeSessionInput{ExternalID: "cse_nul_publication", OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: sessionUUID, SessionExternalID: sid, EnvironmentUUID: env, EnvironmentExternalID: "env_nul", InitialWorkerEpoch: 1, Metadata: []byte(`{"config":{"worker_protocol":"dsr1"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.ListenerPool().Exec(ctx, `UPDATE code_sessions SET worker_lease_expires_at=now()+interval '1 hour' WHERE uuid=$1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	scope := db.DSHScope{OrganizationUUID: org, WorkspaceUUID: workspace, SessionID: sid, CodeSessionID: code.ExternalID, CodeSessionUUID: code.UUID, Epoch: 1}
	objects := &nulPublicationObjects{data: map[string][]byte{}}
	payloads := eventpayload.New(database, objects)
	text := "fresh-prefix中\x00suffix💡 literal\\u0000"
	makeItem := func(id, text string, isError bool) db.DSHPublicEvent {
		raw, err := json.Marshal(map[string]any{"id": id, "type": "agent.tool_result", "tool_use_id": "sevt_actual_tool", "is_error": isError, "content": []any{map[string]any{"type": "text", "text": text}}, "unknown_integer": json.RawMessage(`9007199254740993`)})
		if err != nil {
			t.Fatal(err)
		}
		return db.DSHPublicEvent{ID: id, Payload: raw, Events: []db.SessionEvent{{UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: org, WorkspaceUUID: workspace, SessionExternalID: sid, EventType: "agent.tool_result", CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC(), Payload: raw}}}
	}
	item := makeItem("sevt_nul_result", text, false)
	if len(item.Events[0].Payload) >= eventpayload.Threshold {
		t.Fatal("short payload regression required")
	}
	_, err = database.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{item})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "22P05" {
		t.Fatal("raw bypass must demonstrate actual JSONB NUL failure", err)
	}
	creds, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	service := codesessions.NewServiceWithCredentials(database, creds, nil).WithWorkerEventState(nil, objects)
	h := NewHandler(config.Config{}, database, service, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	writer := &reconnectSSEWriter{header: make(http.Header), ready: make(chan struct{}), release: make(chan struct{}), updates: make(chan struct{}, 1)}
	request := httptest.NewRequest(http.MethodGet, "/events/stream", nil).WithContext(auth.WithPrincipal(ctx, auth.Principal{CredentialType: auth.CredentialTypeAPIKey, OrganizationUUID: org, WorkspaceUUID: workspace}))
	done := make(chan struct{})
	go func() { defer close(done); h.StreamEvents(writer, request, sid) }()
	select {
	case <-writer.ready:
	case <-ctx.Done():
		t.Fatal("SSE did not connect")
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(writer.release) }); cancel(); <-done })
	result, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{item})
	if err != nil || result.Accepted != 1 {
		t.Fatal("lossless short NUL publication", result, err)
	}
	if err = h.PublishCommittedCodeSessionEvents(ctx, code, result.Changes); err != nil {
		t.Fatal(err)
	}
	release.Do(func() { close(writer.release) })
	var wire json.RawMessage
	for wire == nil {
		for _, line := range strings.Split(writer.snapshot(), "\n") {
			if strings.HasPrefix(line, "data: ") {
				var event struct {
					ID string `json:"id"`
				}
				body := []byte(strings.TrimPrefix(line, "data: "))
				if json.Unmarshal(body, &event) == nil && event.ID == item.Events[0].ExternalID {
					wire = bytes.Clone(body)
				}
			}
		}
		if wire != nil {
			break
		}
		select {
		case <-writer.updates:
		case <-ctx.Done():
			t.Fatal("NUL result not delivered through actual SSE handler")
		}
	}
	stored, err := database.GetSessionEvent(ctx, workspace, sid, item.Events[0].ExternalID)
	if err != nil || stored.PayloadBlobUUID == nil {
		t.Fatal("short NUL must use owned blob", err)
	}
	full, err := payloads.GetSessionEvent(ctx, workspace, sid, stored.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	expected := eventPayloadForResponse(full.Payload, full.ProcessedAt, thread.ExternalID)
	equal, err := jsoncanonical.Equal(wire, expected)
	if err != nil || !equal {
		t.Fatal("SSE/history full payload differs", err)
	}
	var content struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Integer json.RawMessage `json:"unknown_integer"`
		IsError bool            `json:"is_error"`
	}
	if err = json.Unmarshal(full.Payload, &content); err != nil || len(content.Content) != 1 || content.Content[0].Text != text || string(content.Integer) != "9007199254740993" || content.IsError {
		t.Fatal("NUL, literal escape, Unicode, numeric precision or result status changed", err)
	}
	var wireFields struct {
		Integer json.RawMessage `json:"unknown_integer"`
	}
	if json.Unmarshal(wire, &wireFields) != nil || string(wireFields.Integer) != "9007199254740993" {
		t.Fatal("SSE lost numeric precision")
	}
	beforeUploads := objects.uploads
	duplicate, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{item})
	if err != nil || duplicate.Duplicates != 1 || duplicate.Accepted != 0 || len(duplicate.Changes.Events) != 0 || objects.uploads != beforeUploads {
		t.Fatal("replay changed identity/payload or uploaded again", duplicate, err)
	}
	t.Run("changed-content-rejected", func(t *testing.T) {
		changed := makeItem(item.ID, text+"changed", false)
		if _, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{changed}); err == nil {
			t.Fatal("changed source accepted")
		}
	})
	t.Run("stale-owner-before-upload", func(t *testing.T) {
		stale := scope
		stale.Epoch--
		before := objects.uploads
		if _, err := payloads.PublishDSHEvents(ctx, stale, []db.DSHPublicEvent{makeItem("sevt_stale", text, false)}); err == nil || objects.uploads != before {
			t.Fatal("stale worker uploaded or published", err)
		}
	})
	t.Run("wrong-workspace", func(t *testing.T) {
		wrong := scope
		wrong.WorkspaceUUID = uuid.NewV4().String()
		if _, err := payloads.PublishDSHEvents(ctx, wrong, []db.DSHPublicEvent{item}); err == nil {
			t.Fatal("foreign scope accepted")
		}
	})
	t.Run("object-integrity", func(t *testing.T) {
		objects.mu.Lock()
		backup := map[string][]byte{}
		for k, v := range objects.data {
			backup[k] = v
			objects.data[k] = append(bytes.Clone(v), 'x')
		}
		objects.mu.Unlock()
		defer func() { objects.mu.Lock(); objects.data = backup; objects.mu.Unlock() }()
		if _, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{item}); err == nil {
			t.Fatal("corrupt blob replay acknowledged")
		}
	})
	t.Run("failed-upload-no-public-receipt", func(t *testing.T) {
		objects.failUpload = true
		defer func() { objects.failUpload = false }()
		failed := makeItem("sevt_upload_failure", text, false)
		if _, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{failed}); err == nil {
			t.Fatal("failed upload acknowledged")
		}
		var receipts int
		if err := database.ListenerPool().QueryRow(ctx, `SELECT count(*) FROM dsh_public_receipts WHERE event_id=$1`, failed.ID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if receipts != 0 {
			t.Fatal("failed upload committed public receipt")
		}
	})
	t.Run("error-result-retained", func(t *testing.T) {
		failed := makeItem("sevt_error_result", text, true)
		if _, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{failed}); err != nil {
			t.Fatal(err)
		}
		event, err := payloads.GetSessionEvent(ctx, workspace, sid, failed.ID)
		if err != nil || !bytes.Contains(event.Payload, []byte(`"is_error":true`)) {
			t.Fatal("error result normalized to success", err)
		}
	})
	t.Run("literal-escape-remains-inline", func(t *testing.T) {
		literal := makeItem("sevt_literal", `literal\u0000`, false)
		if _, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{literal}); err != nil {
			t.Fatal(err)
		}
		event, err := database.GetSessionEvent(ctx, workspace, sid, literal.ID)
		if err != nil || event.PayloadBlobUUID != nil {
			t.Fatal("literal escape mistaken for NUL", err)
		}
	})
	t.Run("concurrent-duplicate", func(t *testing.T) {
		concurrent := makeItem("sevt_concurrent_nul", text, false)
		type outcome struct {
			result db.DSHPublishResult
			err    error
		}
		done := make(chan outcome, 2)
		for range 2 {
			go func() {
				r, e := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{concurrent})
				done <- outcome{r, e}
			}()
		}
		first, second := <-done, <-done
		if first.err != nil || second.err != nil || first.result.Accepted+second.result.Accepted != 1 || first.result.Duplicates+second.result.Duplicates != 1 {
			t.Fatal("concurrent idempotent delivery changed", first.err, second.err, first.result, second.result)
		}
	})
	t.Run("summary-corruption-rejected", func(t *testing.T) {
		if _, err := database.ListenerPool().Exec(ctx, `UPDATE session_events SET payload=jsonb_set(payload,'{preview}','"corrupt"') WHERE external_id=$1 AND workspace_uuid=$2`, item.Events[0].ExternalID, workspace); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := database.ListenerPool().Exec(ctx, `UPDATE session_events SET payload=$1 WHERE external_id=$2 AND workspace_uuid=$3`, stored.Payload, item.Events[0].ExternalID, workspace); err != nil {
				t.Error(err)
			}
		}()
		if _, err := payloads.PublishDSHEvents(ctx, scope, []db.DSHPublicEvent{item}); err == nil {
			t.Fatal("tampered summary replay acknowledged")
		}
	})

}
