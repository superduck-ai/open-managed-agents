package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const dreamWorkerInterval = time.Second

const (
	dreamClaimDuration = 30 * time.Minute
	dreamClaimRenewal  = 10 * time.Minute
	dreamMaxAttempts   = 10
	dreamRetryBase     = 5 * time.Second
	dreamRetryMaximum  = 10 * time.Minute
	dreamWorkerBatch   = 20
)

// Public Dream error types. They follow the Anthropic Dream contract so the
// console and SDK callers can tell an unavailable input apart from a platform
// fault instead of reading free-form messages.
const (
	dreamErrorInternal                     = "internal_error"
	dreamErrorTimeout                      = "timeout"
	dreamErrorInputMemoryStoreUnavailable  = "input_memory_store_unavailable"
	dreamErrorInputSessionUnavailable      = "input_session_unavailable"
	dreamErrorOutputMemoryStoreUnavailable = "output_memory_store_unavailable"
)

// permanentError marks a failure that retrying cannot fix. errorType is the
// public Dream error type persisted in the error column.
type permanentError struct {
	errorType string
	err       error
}

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanentDreamError(err error) error {
	return permanentDreamErrorOfType(dreamErrorInternal, err)
}

func permanentDreamErrorOfType(errorType string, err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{errorType: errorType, err: err}
}

func isPermanentDreamError(err error) bool {
	var target *permanentError
	return errors.As(err, &target)
}

// dreamErrorType resolves the public error type for a terminal failure.
// Transient errors that exhausted their retries are platform faults.
func dreamErrorType(err error) string {
	var target *permanentError
	if errors.As(err, &target) && target.errorType != "" {
		return target.errorType
	}
	return dreamErrorInternal
}

func dreamErrorJSON(cause error) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"type": dreamErrorType(cause), "message": cause.Error()})
}

type dreamDispatchError struct{ err error }

func (e *dreamDispatchError) Error() string { return e.err.Error() }
func (e *dreamDispatchError) Unwrap() error { return e.err }

func dreamRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := dreamRetryBase
	for range attempt - 1 {
		if delay >= dreamRetryMaximum/2 {
			return dreamRetryMaximum
		}
		delay *= 2
	}
	return delay
}

// PendingWorker turns durable pending Dream contracts into runnable internal
// Sessions. Resource setup stays private; public status changes only after the
// unique /dream event has been durably appended and handed to the Session path.
type PendingWorker struct {
	database     *db.DB
	store        storage.ObjectStore
	codeSessions *codesessions.Service
	preparer     *Preparer
	archiver     *InternalSessionArchiver
	logger       *slog.Logger
}

func NewPendingWorker(database *db.DB, storageClient storage.Client, store storage.ObjectStore, codeSessions *codesessions.Service, keepRuntime bool, logger *slog.Logger) *PendingWorker {
	worker := &PendingWorker{
		database: database, store: store, codeSessions: codeSessions,
		preparer: NewPreparer(database, storageClient, store), logger: logging.LoggerOrDefault(logger),
	}
	if !keepRuntime {
		worker.archiver = NewInternalSessionArchiver(database)
	}
	return worker
}

func (w *PendingWorker) Start(ctx context.Context) {
	if w == nil || w.database == nil || w.store == nil {
		return
	}
	workerID := "dream-pending-" + uuid.NewV4().String()
	go func() {
		ticker := time.NewTicker(dreamWorkerInterval)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx, workerID); err != nil {
				w.logger.ErrorContext(ctx, "dream pending worker", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *PendingWorker) RunOnce(ctx context.Context, workerID string) error {
	for range dreamWorkerBatch {
		dream, err := w.database.ClaimNextPendingDream(ctx, workerID, dreamClaimDuration)
		if err != nil {
			return err
		}
		if dream == nil {
			return nil
		}
		if err := w.processWithClaimLease(ctx, *dream, workerID); err != nil {
			if errors.Is(err, errDreamAlreadyTerminal) {
				continue
			}
			var dispatchErr *dreamDispatchError
			if errors.As(err, &dispatchErr) {
				w.logger.WarnContext(ctx, "defer dream command dispatch", "dream_id", dream.ExternalID, "error", dispatchErr.err)
				continue
			}
			w.logger.ErrorContext(ctx, "process pending dream", "dream_id", dream.ExternalID, "attempt", dream.AttemptCount, "error", err)
			if !isPermanentDreamError(err) && dream.AttemptCount < dreamMaxAttempts {
				if _, retryErr := w.database.SchedulePendingDreamRetry(ctx, *dream, workerID, time.Now().UTC().Add(dreamRetryDelay(dream.AttemptCount)), err); retryErr != nil {
					return errors.Join(err, retryErr)
				}
				continue
			}
			if markErr := w.fail(ctx, *dream, workerID, err); markErr != nil {
				return errors.Join(err, markErr)
			}
		}
	}
	return nil
}

func (w *PendingWorker) processWithClaimLease(ctx context.Context, dream db.Dream, workerID string) error {
	processCtx, cancel := context.WithCancel(ctx)
	renewalDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(dreamClaimRenewal)
		defer ticker.Stop()
		for {
			select {
			case <-processCtx.Done():
				renewalDone <- nil
				return
			case <-ticker.C:
				won, err := w.database.RenewPendingDreamClaim(processCtx, dream, workerID, dreamClaimDuration)
				if err != nil {
					renewalDone <- fmt.Errorf("renew Dream claim: %w", err)
					cancel()
					return
				}
				if !won {
					renewalDone <- errors.New("Dream claim ownership was lost")
					cancel()
					return
				}
			}
		}
	}()
	processErr := w.process(processCtx, dream, workerID)
	cancel()
	renewalErr := <-renewalDone
	return errors.Join(processErr, renewalErr)
}

func (w *PendingWorker) process(ctx context.Context, dream db.Dream, workerID string) error {
	if dream.InternalSessionUUID == "" {
		resources, err := w.prepareResources(ctx, dream, workerID)
		if err != nil {
			return err
		}
		dream = resources
	}
	return w.startDream(ctx, workerID, dream)
}

func (w *PendingWorker) prepareResources(ctx context.Context, dream db.Dream, workerID string) (db.Dream, error) {
	if _, err := w.database.GetBuiltinSkillVersion(ctx, "dream", "latest"); err != nil {
		return db.Dream{}, fmt.Errorf("resolve dream skill: %w", err)
	}
	principal := dreamPrincipal(dream)
	input, err := parseStoredDreamInputs(dream.Inputs)
	if err != nil {
		return db.Dream{}, permanentDreamError(err)
	}
	source, err := w.database.GetMemoryStore(ctx, dream.WorkspaceUUID, input.MemoryStoreID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.Dream{}, permanentDreamErrorOfType(dreamErrorInputMemoryStoreUnavailable, fmt.Errorf("input memory store %s was deleted after the Dream was created", input.MemoryStoreID))
		}
		return db.Dream{}, fmt.Errorf("load input memory store: %w", err)
	}
	if source.ArchivedAt != nil {
		return db.Dream{}, permanentDreamErrorOfType(dreamErrorInputMemoryStoreUnavailable, fmt.Errorf("input memory store %s was archived after the Dream was created", input.MemoryStoreID))
	}
	now := time.Now().UTC()
	environment, err := w.preparer.ensureDreamDefaultEnvironment(ctx, principal, now)
	if err != nil {
		return db.Dream{}, fmt.Errorf("ensure dream environment: %w", err)
	}
	agent, err := w.preparer.ensureDreamDefaultAgent(ctx, principal, now)
	if err != nil {
		return db.Dream{}, fmt.Errorf("ensure dream agent: %w", err)
	}
	snapshot, err := dreamSessionAgentSnapshot(agent, dream.Model)
	if err != nil {
		return db.Dream{}, fmt.Errorf("build dream agent snapshot: %w", err)
	}
	output, copiedKeys, err := w.preparer.cloneMemoryStore(ctx, principal, dream, source, now)
	if err != nil {
		return db.Dream{}, fmt.Errorf("clone dream memory store: %w", err)
	}
	committed := false
	var internalSessionID string
	defer func() {
		if committed {
			return
		}
		if internalSessionID != "" {
			_, _ = w.database.DeleteSession(context.WithoutCancel(ctx), dream.WorkspaceUUID, internalSessionID)
		}
		for _, key := range copiedKeys {
			_ = w.store.Delete(context.WithoutCancel(ctx), key, storage.DeleteOptions{})
		}
		_, _ = w.database.ArchiveMemoryStore(context.WithoutCancel(ctx), dream.WorkspaceUUID, output.ExternalID)
	}()
	internalSession, err := w.preparer.createInternalSession(ctx, principal, dream, environment, agent, snapshot, output, now)
	if err != nil {
		return db.Dream{}, fmt.Errorf("create dream session: %w", err)
	}
	internalSessionID = internalSession.ExternalID
	transcripts, err := w.preparer.transcriptRelations(ctx, dream.WorkspaceUUID, dream, input.SessionIDs, now)
	if err != nil {
		return db.Dream{}, fmt.Errorf("bind dream transcripts: %w", err)
	}
	recorded, won, err := w.database.RecordPendingDreamResources(ctx, dream.WorkspaceUUID, dream.ExternalID, workerID, output.UUID, output.ExternalID, internalSession.UUID, internalSession.ExternalID, transcripts, now)
	if err != nil {
		return db.Dream{}, fmt.Errorf("record dream resources: %w", err)
	}
	if !won {
		return db.Dream{}, errDreamAlreadyTerminal
	}
	committed = true
	return recorded, nil
}

func (w *PendingWorker) startDream(ctx context.Context, workerID string, dream db.Dream) error {
	return startDream(ctx, w.database, w.codeSessions, workerID, dream)
}

type dreamStartStore interface {
	GetSession(ctx context.Context, workspaceUUID, externalID string) (db.Session, bool, error)
	GetMemoryStore(ctx context.Context, workspaceUUID, externalID string) (db.MemoryStore, error)
	AppendSessionEventsIfAbsent(ctx context.Context, workspaceUUID, sessionExternalID string, events []db.SessionEvent) ([]db.SessionEvent, error)
	GetSessionEvent(ctx context.Context, workspaceUUID, sessionExternalID, eventExternalID string) (db.SessionEvent, error)
	StartDream(ctx context.Context, dream db.Dream, workerID, outputMemoryStoreID string, session db.Session, event db.SessionEvent, now time.Time) (db.Dream, db.SessionEvent, bool, error)
}

type dreamEventQueue interface {
	QueuePublicSessionEvents(ctx context.Context, session db.Session, events []db.SessionEvent) error
}

func startDream(ctx context.Context, store dreamStartStore, queue dreamEventQueue, workerID string, dream db.Dream) error {
	output, err := dreamOutput(dream.Outputs)
	if err != nil {
		return permanentDreamError(err)
	}
	session, found, err := store.GetSession(ctx, dream.WorkspaceUUID, output.InternalSessionID)
	if err != nil {
		return err
	}
	if !found || session.ArchivedAt != nil {
		return permanentDreamError(errors.New("internal Dream session is unavailable"))
	}
	memoryStore, err := store.GetMemoryStore(ctx, dream.WorkspaceUUID, output.MemoryStoreID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return permanentDreamErrorOfType(dreamErrorOutputMemoryStoreUnavailable, errors.New("output Dream memory store is unavailable"))
		}
		return err
	}
	if memoryStore.ArchivedAt != nil {
		return permanentDreamErrorOfType(dreamErrorOutputMemoryStoreUnavailable, errors.New("output Dream memory store is unavailable"))
	}
	now := time.Now().UTC()
	event, err := dreamCommandEvent(dream, session, now)
	if err != nil {
		return permanentDreamError(err)
	}
	_, command, won, err := store.StartDream(ctx, dream, workerID, output.MemoryStoreID, session, event, now)
	if err != nil {
		return err
	}
	if !won {
		return nil
	}
	if queue == nil {
		return nil
	}
	if err := queue.QueuePublicSessionEvents(ctx, session, []db.SessionEvent{command}); err != nil {
		return &dreamDispatchError{err: fmt.Errorf("queue dream command: %w", err)}
	}
	return nil
}

func interruptDreamSession(ctx context.Context, store dreamStartStore, queue dreamEventQueue, dream db.Dream) error {
	output, err := dreamOutput(dream.Outputs)
	if err != nil {
		return nil
	}
	session, found, err := store.GetSession(ctx, dream.WorkspaceUUID, output.InternalSessionID)
	if err != nil {
		return err
	}
	if !found || session.ArchivedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	event, err := dreamInterruptEvent(dream, session, now)
	if err != nil {
		return err
	}
	created, err := store.AppendSessionEventsIfAbsent(ctx, dream.WorkspaceUUID, session.ExternalID, []db.SessionEvent{event})
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return nil
		}
		return fmt.Errorf("append dream interrupt: %w", err)
	}
	if queue == nil {
		return nil
	}
	if len(created) == 0 {
		existing, loadErr := store.GetSessionEvent(ctx, dream.WorkspaceUUID, session.ExternalID, event.ExternalID)
		if loadErr != nil {
			return fmt.Errorf("load existing dream interrupt: %w", loadErr)
		}
		created = []db.SessionEvent{existing}
	}
	return queue.QueuePublicSessionEvents(ctx, session, created)
}

func (w *PendingWorker) fail(ctx context.Context, dream db.Dream, workerID string, cause error) error {
	errorJSON, err := dreamErrorJSON(cause)
	if err != nil {
		return err
	}
	_, _, err = w.database.MarkClaimedDreamFailed(ctx, dream, workerID, errorJSON, time.Now().UTC())
	if err != nil {
		return err
	}
	if w.archiver == nil {
		return nil
	}
	return w.archiver.RunOnce(ctx)
}

type dreamOutputValue struct {
	MemoryStoreID     string `json:"memory_store_id"`
	InternalSessionID string `json:"internal_session_id"`
}

func dreamOutput(raw json.RawMessage) (dreamOutputValue, error) {
	var values []dreamOutputValue
	if err := json.Unmarshal(raw, &values); err != nil || len(values) != 1 || values[0].MemoryStoreID == "" || values[0].InternalSessionID == "" {
		return dreamOutputValue{}, errors.New("Dream output is incomplete")
	}
	return values[0], nil
}

// failDreamContract closes a running Dream as failed. It reports whether this
// caller won the terminal CAS so follow-up actions (interrupt, archive) only run
// once and never after a concurrent completed/canceled transition.
func failDreamContract(ctx context.Context, database *db.DB, dream db.Dream, cause error, usage json.RawMessage) (bool, error) {
	errorJSON, err := dreamErrorJSON(cause)
	if err != nil {
		return false, err
	}
	if len(usage) == 0 {
		usage = json.RawMessage(`{}`)
	}
	_, won, err := database.MarkDreamTerminal(ctx, dream.WorkspaceUUID, dream.ExternalID, "failed", errorJSON, usage, time.Now().UTC())
	return won, err
}

func dreamPrincipal(dream db.Dream) auth.Principal {
	return auth.Principal{CredentialType: auth.CredentialTypeAPIKey, APIKeyUUID: dream.CreatedByAPIKeyUUID,
		OrganizationUUID: dream.OrganizationUUID, WorkspaceUUID: dream.WorkspaceUUID, UserUUID: dream.RuntimeUserUUID}
}

func dreamCommandEvent(dream db.Dream, session db.Session, now time.Time) (db.SessionEvent, error) {
	eventID := dreamCommandEventID(dream)
	command := "/dream"
	if dream.Instructions != nil && *dream.Instructions != "" {
		command += "\n\n" + *dream.Instructions
	}
	payload, err := json.Marshal(map[string]any{"id": eventID, "type": "user.message", "content": []map[string]string{{"type": "text", "text": command}}, "created_at": now.Format(time.RFC3339), "processed_at": now.Format(time.RFC3339)})
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: eventID, OrganizationUUID: session.OrganizationUUID,
		WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID,
		EventType: "user.message", Payload: payload, ProcessedAt: now, CreatedAt: now}, nil
}

func dreamCommandEventID(dream db.Dream) string {
	return "sevt_" + strings.TrimPrefix(dream.ExternalID, "drm_")
}

func dreamInterruptEvent(dream db.Dream, session db.Session, now time.Time) (db.SessionEvent, error) {
	eventID := "sevt_cancel_" + strings.TrimPrefix(dream.ExternalID, "drm_")
	payload, err := json.Marshal(map[string]any{"id": eventID, "type": "user.interrupt", "created_at": now.Format(time.RFC3339), "processed_at": now.Format(time.RFC3339)})
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: eventID, OrganizationUUID: session.OrganizationUUID,
		WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID,
		EventType: "user.interrupt", Payload: payload, ProcessedAt: now, CreatedAt: now}, nil
}

func containsModelRequestStart(events []db.SessionEvent) bool {
	for _, event := range events {
		if event.EventType == "span.model_request_start" {
			return true
		}
	}
	return false
}

// terminalSessionFailure recognizes a final model request failure emitted by
// the regular Session runner. A retry is not terminal until the runner has
// consumed its advertised retry budget, so transient failures remain running.
func terminalSessionFailure(events []db.SessionEvent) string {
	type systemMessage struct {
		Subtype    string `json:"subtype"`
		Error      string `json:"error"`
		Attempt    int    `json:"attempt"`
		MaxRetries int    `json:"max_retries"`
	}
	type modelRequestEnd struct {
		IsError        bool `json:"is_error"`
		APIErrorStatus int  `json:"api_error_status"`
	}
	for _, event := range events {
		switch event.EventType {
		case "span.model_request_end":
			var end modelRequestEnd
			if json.Unmarshal(event.Payload, &end) != nil || !end.IsError {
				continue
			}
			if end.APIErrorStatus > 0 {
				return fmt.Sprintf("Dream model request failed with HTTP %d", end.APIErrorStatus)
			}
			return "Dream model request failed"
		case "system.message":
			var message systemMessage
			if json.Unmarshal(event.Payload, &message) != nil {
				continue
			}
			if message.Subtype == "api_error" {
				if message.Error == "" {
					return "Dream model request failed"
				}
				return "Dream model request failed: " + message.Error
			}
			if message.Subtype == "api_retry" && message.MaxRetries > 0 && message.Attempt >= message.MaxRetries {
				if message.Error == "" {
					return "Dream model request exhausted retries"
				}
				return fmt.Sprintf("Dream model request exhausted retries: %s", message.Error)
			}
		}
	}
	return ""
}
