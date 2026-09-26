package tunnels

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const payloadCleanupGrace = 5 * time.Minute

type payloadScope struct {
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
}

type payloadReference struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	CleanupJobID string `json:"cleanup_job_id"`
}

type payloadCleanupQueue interface {
	EnqueueScheduledObjectCleanupResourceJob(context.Context, string, string, string, string, string, string, time.Time) error
}

// PayloadStore owns temporary Tunnel bodies; references never cross the connector API.
type PayloadStore struct {
	objects storage.ObjectStore
	cleanup payloadCleanupQueue
}

func NewPayloadStore(database *db.DB, objects storage.ObjectStore) *PayloadStore {
	return &PayloadStore{objects: objects, cleanup: database}
}

func payloadObjectKey(scope payloadScope, requestID, jobID string) string {
	// Hash trusted routing identities so none can change the object's path prefix.
	return "tunnel-payload/" + brokerKey(scope.OrganizationUUID, scope.WorkspaceUUID, requestID) + "/" + jobID + ".json"
}

func (s *PayloadStore) save(ctx context.Context, scope payloadScope, requestID string, deadline time.Time, body json.RawMessage, limit int64) (*payloadReference, error) {
	if s == nil || s.objects == nil || s.cleanup == nil {
		return nil, errPayloadStorageUnavailable
	}
	if scope.OrganizationUUID == "" || scope.WorkspaceUUID == "" || requestID == "" {
		return nil, errPayloadReferenceInvalid
	}
	if len(body) == 0 || int64(len(body)) > limit {
		return nil, ErrPayloadLimit
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobID, err := ids.New("job_")
	if err != nil {
		return nil, err
	}
	key := payloadObjectKey(scope, requestID, jobID)
	if err := s.cleanup.EnqueueScheduledObjectCleanupResourceJob(ctx, jobID, scope.WorkspaceUUID, s.objects.Name(), key, "tunnel_payload", requestID, deadline.Add(payloadCleanupGrace)); err != nil {
		return nil, fmt.Errorf("schedule tunnel payload cleanup: %w", err)
	}
	result, err := s.objects.Upload(ctx, key, bytes.NewReader(body), storage.UploadOptions{Size: int64(len(body)), ContentType: "application/json"})
	if err != nil {
		return nil, fmt.Errorf("upload tunnel payload: %w", err)
	}
	if result.Size != int64(len(body)) {
		return nil, errPayloadSizeMismatch
	}
	digest := sha256.Sum256(body)
	return &payloadReference{Key: key, Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:]), CleanupJobID: jobID}, nil
}

func (s *PayloadStore) restore(ctx context.Context, scope payloadScope, requestID string, body json.RawMessage, ref *payloadReference, limit int64) (json.RawMessage, error) {
	if ref == nil {
		return body, nil
	}
	if s == nil || s.objects == nil {
		return nil, errPayloadStorageUnavailable
	}
	if len(body) != 0 || ref.Size <= 0 || ref.Size > limit || scope.OrganizationUUID == "" || scope.WorkspaceUUID == "" || requestID == "" ||
		len(ref.CleanupJobID) != 28 || ref.CleanupJobID[:4] != "job_" ||
		ref.Key != payloadObjectKey(scope, requestID, ref.CleanupJobID) {
		return nil, errPayloadReferenceInvalid
	}
	// The generated ID suffix must not contain path separators.
	for _, ch := range ref.CleanupJobID[4:] {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z') {
			return nil, errPayloadReferenceInvalid
		}
	}
	object, err := s.objects.Open(ctx, ref.Key, nil)
	if err != nil {
		return nil, fmt.Errorf("open tunnel payload: %w", err)
	}
	defer object.Body.Close()
	if object.Size >= 0 && object.Size != ref.Size {
		return nil, errPayloadSizeMismatch
	}
	data, err := io.ReadAll(io.LimitReader(object.Body, ref.Size+1))
	if err != nil {
		return nil, fmt.Errorf("read tunnel payload: %w", err)
	}
	if int64(len(data)) != ref.Size {
		return nil, errPayloadSizeMismatch
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return nil, errPayloadDigestMismatch
	}
	return data, nil
}
