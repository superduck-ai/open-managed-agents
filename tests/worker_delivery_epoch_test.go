package tests

import (
	"context"
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
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

// Pause after the delivery handler validates the epoch, before it can ACK.
// Credential rotation must still be blocked here by the same PG row lock.
type pausedDeliveryAckStore struct {
	*workerevents.MemoryAcknowledgementStore
	entered chan struct{}
	release chan struct{}
}

func (s *pausedDeliveryAckStore) GetMany(ctx context.Context, sessionID string, epoch int64, eventIDs []string) (map[string]workerevents.AckRef, error) {
	close(s.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return s.MemoryAcknowledgementStore.GetMany(ctx, sessionID, epoch, eventIDs)
	}
}

func TestWorkerDeliverySerializesEpochRotation(t *testing.T) {
	for _, status := range []string{"cancelled", "received", "processing", "processed"} {
		t.Run(status, func(t *testing.T) {
			app := newTestAppWithStore(t, nil, newFakeStore("delivery-epoch-lock-"+status))
			defer app.close()
			agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"delivery-epoch-lock"}`)
			defer cleanupAgentRows(t, app.pool, agent.ID)
			env := createEnvironment(t, app, `{"name":"delivery-epoch-lock"}`)
			defer cleanupEnvironmentRows(t, app.pool, env.ID)
			session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			defer deleteSession(t, app, session.ID)
			codeSessionID := launchLocalCodeSession(t, app, session.ID)
			registerCodeSessionWorker(t, app, codeSessionID)
			codeSession, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			verifyDeliveryEpochLock(t, app, codeSession, status)
		})
	}
}

func verifyDeliveryEpochLock(t *testing.T, app *testApp, codeSession db.CodeSession, status string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	subscription, err := app.workerEvents.Subscribe(ctx, codeSession.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	var delivery workerevents.Delivery
	select {
	case delivery = <-subscription.Messages():
	case <-ctx.Done():
		t.Fatal("initialize was not delivered")
	}
	acks := &pausedDeliveryAckStore{
		MemoryAcknowledgementStore: workerevents.NewMemoryAcknowledgementStore(),
		entered:                    make(chan struct{}),
		release:                    make(chan struct{}),
	}
	epoch := codeSession.CurrentWorkerEpoch
	if err := acks.Put(ctx, codeSession.ExternalID, epoch, delivery.Envelope.EventID, workerevents.AckRef{AckSubject: delivery.AckSubject}); err != nil {
		t.Fatal(err)
	}
	service := newCodeSessionService(app, nil, acks)
	router := chi.NewRouter()
	codesessions.NewHandler(app.cfg, service, nil, nil).RegisterV1Routes(router)
	updateStatus := status
	if status == "cancelled" {
		updateStatus = "processed"
	}
	body := `{"worker_epoch":` + quoteJSON(strconv.FormatInt(epoch, 10)) + `,"updates":[{"event_id":` + quoteJSON(delivery.Envelope.EventID) + `,"status":` + quoteJSON(updateStatus) + `}]}`
	request := httptest.NewRequest(http.MethodPost, "/code/sessions/"+codeSession.ExternalID+"/worker/events/delivery", strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+codeSessionIngressToken(t, app, codeSession.ExternalID))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(response, request)
	}()
	defer func() { cancel(); <-done }()
	select {
	case <-acks.entered:
	case <-ctx.Done():
		t.Fatal("delivery did not reach ACK lookup")
	}
	owner := db.Session{OrganizationUUID: codeSession.OrganizationUUID, WorkspaceUUID: codeSession.WorkspaceUUID, ExternalID: codeSession.SessionExternalID}
	replacementHash := "replacement-" + codeSession.ExternalID
	rotationCtx, stopRotation := context.WithTimeout(ctx, 200*time.Millisecond)
	_, rotationErr := app.db.RotateManagedAgentCodeSessionCredentials(rotationCtx, owner, codeSession.ExternalID, replacementHash)
	stopRotation()
	if !errors.Is(rotationErr, context.DeadlineExceeded) {
		t.Fatalf("rotation during delivery = %v, want blocked until deadline", rotationErr)
	}
	if status == "cancelled" {
		cancel()
	} else {
		close(acks.release)
	}
	<-done
	if status != "cancelled" && response.Code != http.StatusOK {
		t.Fatalf("delivery status = %d: %s", response.Code, response.Body.String())
	}
	if status == "cancelled" && response.Code == http.StatusOK {
		t.Fatal("cancelled delivery succeeded")
	}
	wantPending := 1
	if status == "processed" {
		wantPending = 0
	}
	if pending := app.workerEvents.Pending(codeSession.ExternalID); len(pending) != wantPending {
		t.Fatalf("pending messages = %d, want %d", len(pending), wantPending)
	}
	rotationCtx, stopRotation = context.WithTimeout(t.Context(), 2*time.Second)
	defer stopRotation()
	nextEpoch, err := app.db.RotateManagedAgentCodeSessionCredentials(rotationCtx, owner, codeSession.ExternalID, replacementHash)
	// The timed-out UPDATE may race server-side cancellation after the lock is
	// released. Its outcome is ambiguous, but rotation must now make progress.
	if err != nil || nextEpoch <= epoch {
		t.Fatalf("rotation after delivery = (%d, %v), want epoch greater than %d", nextEpoch, err, epoch)
	}
	called := false
	err = app.db.WithLockedCodeSessionWorkerEpoch(rotationCtx, codeSession.ExternalID, epoch, func() (bool, error) {
		called = true
		return true, nil
	})
	if !errors.Is(err, db.ErrWorkerEpochMismatch) || called {
		t.Fatalf("stale delivery = (%v, callback=%t), want epoch mismatch before callback", err, called)
	}
}
