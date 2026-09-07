package codesessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type preparedInboundEvent struct {
	messageID    string
	envelope     workerevents.EnvelopeV1
	cleanupJobID string
}

func (s *Service) prepareInboundEvent(
	ctx context.Context,
	codeSession db.CodeSession,
	payload json.RawMessage,
	source string,
	stableSeed string,
) (preparedInboundEvent, error) {
	metadata, err := BuildEventMetadata(codeSession.ExternalID, "inbound", payload)
	if err != nil {
		return preparedInboundEvent{}, err
	}
	if stableSeed == "" && (metadata.PayloadUUID != nil || metadata.RequestID != nil) {
		stableSeed = metadata.IdempotencyKey
	}
	eventID, err := inboundEventID(codeSession.ExternalID, stableSeed)
	if err != nil {
		return preparedInboundEvent{}, err
	}

	payloadEventID := ""
	if metadata.PayloadUUID != nil {
		payloadEventID = *metadata.PayloadUUID
	}
	expiresAt := time.Now().UTC().Add(workerevents.LogicalRetention)
	envelope := workerevents.EventEnvelope(
		codeSession.ExternalID,
		eventID,
		payloadEventID,
		metadata.EventType,
		metadata.EventSubtype,
		metadata.Payload,
		expiresAt,
	)
	cleanupJobID := ""
	// 按 payload 长度估算外置阈值，全量编码留给 Publish；未外置的 envelope 加上
	// 固定字段余量也远小于单条消息上限。
	if workerevents.LikelyExceedsLargePayload(len(metadata.Payload)) {
		cleanupJobID, err = s.offloadInboundPayload(ctx, codeSession, source, eventID, expiresAt, &envelope)
		if err != nil {
			return preparedInboundEvent{}, err
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			s.triggerPayloadCleanupNow(ctx, cleanupJobID)
			return preparedInboundEvent{}, err
		}
		if len(encoded) > workerevents.MaxMessageBytes {
			s.triggerPayloadCleanupNow(ctx, cleanupJobID)
			return preparedInboundEvent{}, fmt.Errorf("worker event envelope exceeds %d bytes", workerevents.MaxMessageBytes)
		}
	}
	return preparedInboundEvent{messageID: eventID, envelope: envelope, cleanupJobID: cleanupJobID}, nil
}

func inboundEventID(codeSessionID, stableSeed string) (string, error) {
	if stableSeed == "" {
		return ids.New("csev_")
	}
	digest := sha256.Sum256([]byte(codeSessionID + "\x00inbound\x00" + stableSeed))
	return "csev_" + hex.EncodeToString(digest[:16]), nil
}

func (s *Service) offloadInboundPayload(
	ctx context.Context,
	codeSession db.CodeSession,
	source string,
	eventID string,
	expiresAt time.Time,
	envelope *workerevents.EnvelopeV1,
) (string, error) {
	if s.workerEventObjects == nil {
		return "", errorsLargePayloadStorageUnavailable()
	}
	cleanupJobID, err := ids.New("job_")
	if err != nil {
		return "", err
	}
	objectKey := strings.Join([]string{
		"worker-inbound", codeSession.OrganizationUUID, codeSession.WorkspaceUUID,
		codeSession.ExternalID, eventID, cleanupJobID + ".json",
	}, "/")
	if err := s.db.EnqueueScheduledObjectCleanupResourceJob(
		ctx, cleanupJobID, codeSession.WorkspaceUUID, s.workerEventObjects.Name(), objectKey,
		"worker_inbound_payload", eventID, expiresAt,
	); err != nil {
		return "", err
	}
	payload := bytes.Clone(envelope.Payload)
	result, err := s.workerEventObjects.Upload(ctx, objectKey, bytes.NewReader(payload), storage.UploadOptions{
		Size: int64(len(payload)), ContentType: "application/json",
	})
	if err != nil {
		s.triggerPayloadCleanupNow(ctx, cleanupJobID)
		return "", fmt.Errorf("upload %s worker event payload: %w", source, err)
	}
	if result.Size != int64(len(payload)) {
		s.triggerPayloadCleanupNow(ctx, cleanupJobID)
		return "", fmt.Errorf("upload worker event payload size mismatch: got %d, want %d", result.Size, len(payload))
	}
	digest := sha256.Sum256(payload)
	envelope.Payload = nil
	envelope.PayloadRef = &workerevents.PayloadReference{
		Key: objectKey, Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]), CleanupJobID: cleanupJobID,
	}
	return cleanupJobID, nil
}

// loadOffloadedPayload restores an envelope whose payload was offloaded to
// object storage, downloading and integrity-checking it via PayloadRef.
func (s *Service) loadOffloadedPayload(ctx context.Context, envelope workerevents.EnvelopeV1) (workerevents.EnvelopeV1, error) {
	if envelope.PayloadRef == nil {
		return envelope, nil
	}
	if s.workerEventObjects == nil {
		return workerevents.EnvelopeV1{}, errorsLargePayloadStorageUnavailable()
	}
	reference := envelope.PayloadRef
	if reference.Size < 0 || reference.Size > workerevents.MaxOffloadedPayloadBytes {
		return workerevents.EnvelopeV1{}, fmt.Errorf("worker event payload reference has invalid size %d", reference.Size)
	}
	object, err := s.workerEventObjects.Open(ctx, reference.Key, nil)
	if err != nil {
		return workerevents.EnvelopeV1{}, fmt.Errorf("open worker event payload: %w", err)
	}
	defer object.Body.Close()
	if object.Size >= 0 && object.Size != reference.Size {
		return workerevents.EnvelopeV1{}, fmt.Errorf("worker event payload object size mismatch: got %d, want %d", object.Size, reference.Size)
	}
	payload, err := io.ReadAll(io.LimitReader(object.Body, reference.Size+1))
	if err != nil {
		return workerevents.EnvelopeV1{}, fmt.Errorf("read worker event payload: %w", err)
	}
	if int64(len(payload)) != reference.Size {
		return workerevents.EnvelopeV1{}, fmt.Errorf("worker event payload size mismatch: got %d, want %d", len(payload), reference.Size)
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != reference.SHA256 {
		return workerevents.EnvelopeV1{}, errorsLargePayloadDigestMismatch()
	}
	envelope.Payload = payload
	return envelope, nil
}

// triggerPayloadCleanupNow moves a payload's scheduled cleanup job forward so
// it runs immediately. It is best-effort: failures are only logged.
func (s *Service) triggerPayloadCleanupNow(ctx context.Context, cleanupJobID string) {
	if cleanupJobID == "" {
		return
	}
	if err := s.db.ExpediteObjectCleanupJob(ctx, cleanupJobID); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.logger.WarnContext(ctx, "trigger worker event payload cleanup now", "cleanup_job_id", cleanupJobID, "error", err)
	}
}

func errorsLargePayloadStorageUnavailable() error {
	return fmt.Errorf("worker event payload object storage is unavailable")
}

func errorsLargePayloadDigestMismatch() error {
	return fmt.Errorf("worker event payload digest mismatch")
}
