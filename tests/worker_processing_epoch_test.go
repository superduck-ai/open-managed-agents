//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type rotatingProcessingSink struct {
	*sessionsapi.Handler
	rotate func() error
}

func (s *rotatingProcessingSink) ProcessCodeSessionInputs(ctx context.Context, code db.CodeSession, epoch int64, inputIDs []string) error {
	if err := s.rotate(); err != nil {
		return err
	}
	return s.Handler.ProcessCodeSessionInputs(ctx, code, epoch, inputIDs)
}

func (s *rotatingProcessingSink) PublishCodeSessionEvents(ctx context.Context, code db.CodeSession, payloads []json.RawMessage) error {
	if err := s.rotate(); err != nil {
		return err
	}
	return s.Handler.PublishCodeSessionEvents(ctx, code, payloads)
}

func processingEpochFixture(t *testing.T) (*testApp, db.CodeSession, string) {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("processing-epoch"))
	code, _ := newPayloadIntegrationSession(t, app)
	sent := sendSessionEvents(t, app, code.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`, defaultTestKey)
	return app, code, sessionEventStringField(t, sent.Data[0], "id")
}

func TestWorkerProcessingRejectsEpochRotatedAfterDeliveryLookup(t *testing.T) {
	app, code, inputID := processingEpochFixture(t)
	acks := workerevents.NewMemoryAcknowledgementStore()
	if err := acks.Put(t.Context(), code.ExternalID, code.CurrentWorkerEpoch, "old-delivery", workerevents.AckRef{AckSubject: "old-ack", PublicEventID: inputID}); err != nil {
		t.Fatal(err)
	}
	service := newCodeSessionService(app, workerevents.NewMemory(), acks)
	service.SetPublicEventSink(&rotatingProcessingSink{
		Handler: sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil),
		rotate: func() error {
			_, err := app.db.RotateManagedAgentCodeSessionCredentials(t.Context(), db.Session{OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, ExternalID: code.SessionExternalID}, code.ExternalID, "replacement-processing-token")
			return err
		},
	})
	router := chi.NewRouter()
	codesessions.NewHandler(app.cfg, service, nil, nil).RegisterV1Routes(router)
	request := httptest.NewRequest(http.MethodPost, "/code/sessions/"+code.ExternalID+"/worker/events/delivery", strings.NewReader(`{"worker_epoch":`+quoteJSON(strconv.FormatInt(code.CurrentWorkerEpoch, 10))+`,"updates":[{"event_id":"old-delivery","status":"processing"}]}`))
	request.Header.Set("Authorization", "Bearer "+codeSessionIngressToken(t, app, code.ExternalID))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale processing status = %d: %s", response.Code, response.Body.String())
	}
	event, err := app.db.GetSessionEvent(t.Context(), code.WorkspaceUUID, code.SessionExternalID, inputID)
	if err != nil || !event.ProcessedAt.IsZero() {
		t.Fatalf("old epoch processed input: %+v %v", event, err)
	}
}

func TestWorkerProcessingHoldsEpochLockUntilInputCommit(t *testing.T) {
	app, code, inputID := processingEpochFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	lock, err := app.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(ctx, `SELECT uuid FROM session_events WHERE workspace_uuid = $1 AND session_external_id = $2 AND external_id = $3 FOR UPDATE`, code.WorkspaceUUID, code.SessionExternalID, inputID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		events, err := app.db.ProcessSessionInputsForWorker(ctx, code, code.CurrentWorkerEpoch, []string{inputID, inputID})
		if err == nil && len(events) != 1 {
			err = errors.New("expected exactly one processing update")
		}
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := app.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%UPDATE session_events SET processed_at%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	rotationCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = app.db.RotateManagedAgentCodeSessionCredentials(rotationCtx, db.Session{OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, ExternalID: code.SessionExternalID}, code.ExternalID, "replacement-processing-lock")
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("epoch rotation bypassed processing transaction: %v", err)
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	event, err := app.db.GetSessionEvent(ctx, code.WorkspaceUUID, code.SessionExternalID, inputID)
	if err != nil || event.ProcessedAt.IsZero() {
		t.Fatalf("input not committed: %+v %v", event, err)
	}
}

func TestWorkerProcessingClearsIdleSinceAndKeepsDuplicateTime(t *testing.T) {
	app, code, inputID := processingEpochFixture(t)
	_, err := app.db.UpdateCodeSessionWorkerState(t.Context(), code.ExternalID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: code.CurrentWorkerEpoch, WorkerStatus: new("idle")})
	if err != nil {
		t.Fatal(err)
	}
	idleSince := func() *time.Time {
		t.Helper()
		var at *time.Time
		if err := app.pool.QueryRow(t.Context(), `SELECT idle_since FROM code_sessions WHERE workspace_uuid = $1 AND external_id = $2`, code.WorkspaceUUID, code.ExternalID).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	if idleSince() == nil {
		t.Fatal("idle fixture has no timer")
	}
	events, err := app.db.ProcessSessionInputsForWorker(t.Context(), code, code.CurrentWorkerEpoch, []string{inputID, inputID, "missing"})
	if err != nil || len(events) != 1 {
		t.Fatalf("processing updates = %d, %v", len(events), err)
	}
	if idleSince() != nil {
		t.Fatal("processing retained idle timer")
	}
	again, err := app.db.ProcessSessionInputsForWorker(t.Context(), code, code.CurrentWorkerEpoch, []string{inputID})
	if err != nil || len(again) != 0 {
		t.Fatalf("duplicate receipt produced updates: %d %v", len(again), err)
	}
	event, err := app.db.GetSessionEvent(t.Context(), code.WorkspaceUUID, code.SessionExternalID, inputID)
	if err != nil || !event.ProcessedAt.Equal(events[0].ProcessedAt) {
		t.Fatalf("duplicate changed processing time: %+v %v", event, err)
	}
}
