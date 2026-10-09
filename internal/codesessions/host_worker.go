package codesessions

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type HostWorker struct {
	service      *Service
	record       db.CodeSession
	epoch        int64
	subscription workerevents.Subscription
}

type HostInput struct {
	EventID string
	Payload json.RawMessage
}

func isHostCodeSession(record db.CodeSession) bool {
	var metadata struct {
		Config struct {
			AgentRuntimeMode string `json:"agent_runtime_mode"`
		} `json:"config"`
	}
	return json.Unmarshal(record.Metadata, &metadata) == nil && metadata.Config.AgentRuntimeMode == "host"
}

func (s *Service) OpenHostWorker(ctx context.Context, codeSessionID string, epoch int64) (*HostWorker, error) {
	record, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	if !isHostCodeSession(record) {
		return nil, ErrNotHostWorker
	}
	if record.Status != "active" {
		return nil, db.ErrInvalidState
	}
	binding := db.CodeSessionWorkerBinding{TokenSessionID: codeSessionID, AuthMode: "host_runtime", Subject: codeSessionID, Issuer: "oma"}
	registeredEpoch, _, err := s.db.RegisterCodeSessionWorkerAtEpoch(ctx, codeSessionID, epoch, binding, codeSessionWorkerLeaseTTL)
	if err != nil {
		return nil, err
	}
	worker := &HostWorker{service: s, record: record, epoch: registeredEpoch}
	if err := s.db.MarkCodeSessionWorkerConnectedForEpoch(ctx, codeSessionID, registeredEpoch); err != nil {
		return nil, err
	}
	subscription, err := s.workerEvents.Subscribe(ctx, codeSessionID)
	if err != nil {
		return nil, worker.closeConnection(ctx, err)
	}
	worker.subscription = subscription
	return worker, nil
}

func (w *HostWorker) Next(ctx context.Context) (HostInput, error) {
	select {
	case <-ctx.Done():
		return HostInput{}, ctx.Err()
	case err, open := <-w.subscription.Errors():
		if !open || err == nil {
			return HostInput{}, io.EOF
		}
		return HostInput{}, err
	case delivery, open := <-w.subscription.Messages():
		if !open {
			return HostInput{}, io.EOF
		}
		return w.accept(ctx, delivery)
	}
}

func (w *HostWorker) accept(ctx context.Context, delivery workerevents.Delivery) (HostInput, error) {
	if err := w.service.db.ValidateCodeSessionWorkerEpoch(ctx, w.record.ExternalID, w.epoch); err != nil {
		return HostInput{}, err
	}
	envelope := delivery.Envelope
	if envelope.Version != 2 || envelope.CodeSessionID != w.record.ExternalID {
		return HostInput{}, ErrProtocol
	}
	if envelope.IsExpired(time.Now().UTC()) {
		if err := w.service.expireWorkerEvent(ctx, w.record, envelope); err != nil {
			return HostInput{}, err
		}
		return HostInput{}, ErrHostInputExpired
	}
	envelope, err := w.service.loadOffloadedPayload(ctx, envelope)
	if err != nil {
		return HostInput{}, err
	}
	eventID := codeSessionWorkerSSEEventID(codeSessionEventFromWorkerEnvelope(envelope))
	var publicEvent struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Payload, &publicEvent); err != nil {
		return HostInput{}, err
	}
	reference := workerevents.AckRef{AckSubject: delivery.AckSubject, PublicEventID: publicEvent.ID}
	if envelope.PayloadRef != nil {
		reference.CleanupJobID = envelope.PayloadRef.CleanupJobID
	}
	if err := w.service.workerEventAcks.Put(ctx, w.record.ExternalID, w.epoch, eventID, reference); err != nil {
		return HostInput{}, err
	}
	return HostInput{EventID: eventID, Payload: envelope.Payload}, nil
}

func (w *HostWorker) Acknowledge(ctx context.Context, eventID, status string) error {
	_, err := w.service.applyWorkerDeliveryUpdates(ctx, w.record.ExternalID, w.epoch, []workerDeliveryUpdate{{EventID: eventID, Status: status}})
	return err
}

func (w *HostWorker) Heartbeat(ctx context.Context) error {
	_, err := w.service.db.RecordCodeSessionWorkerHeartbeat(ctx, w.record.ExternalID, w.epoch, codeSessionWorkerLeaseTTL, codeSessionWorkerLeaseGrace)
	if err != nil {
		return err
	}
	return w.service.resumeSandboxForCodeSession(ctx, w.record)
}

func (w *HostWorker) SetState(ctx context.Context, status string) error {
	if status != "running" && status != "idle" && status != "requires_action" {
		return ErrProtocol
	}
	record, err := w.service.db.UpdateCodeSessionWorkerState(ctx, w.record.ExternalID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: w.epoch, WorkerStatus: &status})
	if err != nil {
		return err
	}
	return w.service.syncPublicSessionFromWorker(ctx, record, status)
}

func (w *HostWorker) EndTurn(ctx context.Context, failed bool) error {
	status := "idle"
	record, err := w.service.db.UpdateCodeSessionWorkerState(ctx, w.record.ExternalID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: w.epoch, WorkerStatus: &status})
	if err != nil {
		return err
	}
	if !failed {
		return w.service.syncPublicSessionFromWorker(ctx, record, status)
	}
	payload, err := hostFailedTurnPayload(time.Now().UTC())
	if err != nil {
		return err
	}
	return w.Publish(ctx, payload)
}

type hostTurnStopReason struct {
	Type string `json:"type"`
}

type hostFailedTurnEvent struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"`
	ProcessedAt string             `json:"processed_at"`
	StopReason  hostTurnStopReason `json:"stop_reason"`
}

func hostFailedTurnPayload(at time.Time) (json.RawMessage, error) {
	eventID, err := ids.New("sevt_")
	if err != nil {
		return nil, err
	}
	return json.Marshal(hostFailedTurnEvent{
		ID: eventID, Type: "session.status_idle", ProcessedAt: formatTime(at),
		StopReason: hostTurnStopReason{Type: "retries_exhausted"},
	})
}

func (w *HostWorker) Publish(ctx context.Context, payload json.RawMessage) error {
	var err error
	payload, err = hostPublicPayload(payload)
	if err != nil {
		return err
	}
	var header workerPayloadHeader
	if err := json.Unmarshal(payload, &header); err != nil {
		return err
	}
	if maevents.IsStreamDelta(header.Type) {
		if err := w.service.db.TouchCodeSessionWorkerActivityForEpoch(ctx, w.record.ExternalID, w.epoch); err != nil {
			return err
		}
		return w.publishPreview(ctx, payload)
	}
	return w.service.AppendWorkerEventForEpoch(ctx, CodeSessionStreamRoute{CodeSessionID: w.record.ExternalID, WorkspaceUUID: w.record.WorkspaceUUID, SessionExternalID: w.record.SessionExternalID}, w.epoch, payload)
}

func (w *HostWorker) publishPreview(ctx context.Context, payload json.RawMessage) error {
	if w.service.sink == nil {
		return db.ErrInvalidState
	}
	return w.service.sink.PublishCodeSessionStreamEvent(ctx, CodeSessionStreamRoute{
		CodeSessionID: w.record.ExternalID, WorkspaceUUID: w.record.WorkspaceUUID, SessionExternalID: w.record.SessionExternalID,
	}, w.epoch, payload)
}

func hostPublicPayload(raw json.RawMessage) (json.RawMessage, error) {
	var identity struct {
		ID      string `json:"id"`
		EventID string `json:"event_id"`
		Event   struct {
			ID string `json:"id"`
		} `json:"event"`
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &identity); err != nil {
		return nil, ErrProtocol
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope == nil {
		return nil, ErrProtocol
	}
	id := firstNonEmpty(identity.ID, identity.EventID, identity.Event.ID)
	if id == "" {
		return nil, ErrProtocol
	}
	uuid, err := json.Marshal(id)
	if err != nil {
		return nil, err
	}
	envelope["uuid"] = uuid
	return json.Marshal(envelope)
}

func (w *HostWorker) closeConnection(ctx context.Context, prior error) error {
	if err := w.service.db.MarkCodeSessionWorkerDisconnectedForEpoch(ctx, w.record.ExternalID, w.epoch); prior == nil {
		return err
	}
	return prior
}

func (w *HostWorker) Close(ctx context.Context) error {
	var err error
	if w.subscription != nil {
		err = w.subscription.Close()
	}
	return w.closeConnection(ctx, err)
}

func HostRunID(codeSessionID, inputEventID string) string {
	return stablePublicEventID(codeSessionID, "host-run:"+inputEventID)
}
