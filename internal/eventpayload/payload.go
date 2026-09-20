// Package eventpayload owns event JSON storage and full-payload restoration.
package eventpayload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const (
	Threshold     = storage.EventPayloadThreshold
	PreviewBytes  = 512
	MaxBytes      = 16 * 1024 * 1024
	uploadTimeout = 2 * time.Minute
)

type Reference struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
}

type Summary struct {
	Type    string     `json:"type"`
	Name    *string    `json:"name"`
	BlobRef *Reference `json:"blob_ref"`
	Size    int64      `json:"size"`
	Preview string     `json:"preview"`
}

type payloadMetadata struct {
	Name            json.RawMessage `json:"name"`
	ToolUseID       json.RawMessage `json:"tool_use_id"`
	MCPToolUseID    json.RawMessage `json:"mcp_tool_use_id"`
	CustomToolUseID json.RawMessage `json:"custom_tool_use_id"`
	ID              json.RawMessage `json:"id"`
}

// Store holds stable database and object storage dependencies at the service boundary.
type Store struct {
	database *db.DB
	objects  storage.ObjectStore
}

func New(database *db.DB, objects storage.ObjectStore) *Store {
	return &Store{database: database, objects: objects}
}

func ExceedsThreshold(size int) bool { return size > Threshold }

func Preview(payload []byte) string {
	if len(payload) > PreviewBytes {
		payload = payload[:PreviewBytes]
	}
	for len(payload) > 0 && !utf8.Valid(payload) {
		payload = payload[:len(payload)-1]
	}
	return string(payload)
}

func Summarize(payload []byte, eventType string) (Summary, *string, error) {
	var metadata payloadMetadata
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return Summary{}, nil, err
	}
	var toolID *string
	for _, candidate := range []json.RawMessage{metadata.ToolUseID, metadata.MCPToolUseID, metadata.CustomToolUseID, metadata.ID} {
		if value := metadataString(candidate); value != nil {
			toolID = value
			break
		}
	}
	return Summary{Type: eventType, Name: metadataString(metadata.Name), Size: int64(len(payload)), Preview: Preview(payload)}, toolID, nil
}

// Metadata is optional; opaque worker JSON may use these names for non-string values.
func metadataString(raw json.RawMessage) *string {
	var value *string
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}

func (s *Store) prepare(ctx context.Context, organizationUUID, workspaceUUID, identity string, payload []byte, summary Summary) ([]byte, *string, error) {
	if !ExceedsThreshold(len(payload)) {
		return payload, nil, nil
	}
	if len(payload) > MaxBytes {
		return nil, nil, errPayloadTooLarge
	}
	if s.objects == nil {
		return nil, nil, errStorageUnavailable
	}
	if preparation, ok := ctx.Value(preparationContextKey{}).(*transactionPreparation); ok {
		key := preparedPayloadKey{workspaceUUID: workspaceUUID, identity: identity, eventType: summary.Type, digest: sha256.Sum256(payload)}
		if prepared, found := preparation.payloads[key]; found {
			return prepared.payload, prepared.blobUUID, nil
		}
		return nil, nil, &preparationRequired{store: s, organizationUUID: organizationUUID, workspaceUUID: workspaceUUID, identity: identity, payload: payload, summary: summary}
	}
	blobUUID := uuid.NewV4().String()
	digest := sha256.Sum256(payload)
	blob := db.EventPayloadBlob{
		UUID: blobUUID, ExternalID: "epb_" + blobUUID,
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID,
		Bucket: s.objects.Name(), Key: "event-payload/" + organizationUUID + "/" + workspaceUUID + "/" + blobUUID + ".json",
		Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]),
	}
	if err := s.database.RegisterEventPayloadBlob(ctx, blob); err != nil {
		return nil, nil, err
	}
	uploadCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	result, err := s.objects.Upload(uploadCtx, blob.Key, bytes.NewReader(payload), storage.UploadOptions{Size: blob.Size, ContentType: "application/json"})
	if err != nil {
		return nil, nil, fmt.Errorf("upload event payload: %w", err)
	}
	if result.Size != blob.Size {
		return nil, nil, errSizeMismatch
	}
	summary.BlobRef = &Reference{Version: 1, ID: blob.ExternalID}
	encoded, err := json.Marshal(summary)
	return encoded, &blobUUID, err
}

func (s *Store) restore(ctx context.Context, workspaceUUID string, payload []byte, blobUUID *string) ([]byte, error) {
	if blobUUID == nil {
		return payload, nil
	}
	if s.objects == nil {
		return nil, errStorageUnavailable
	}
	blob, err := s.database.GetEventPayloadBlob(ctx, workspaceUUID, *blobUUID)
	if err != nil {
		return nil, err
	}
	if blob.Bucket != s.objects.Name() {
		return nil, errStorageUnavailable
	}
	return readBlob(ctx, s.objects, blob)
}

func (s *Store) restoreTx(ctx context.Context, tx db.ManagedAgentEventTx, workspaceUUID string, payload []byte, blobUUID *string) ([]byte, error) {
	if blobUUID == nil {
		return payload, nil
	}
	if s.objects == nil {
		return nil, errStorageUnavailable
	}
	blob, err := tx.GetEventPayloadBlob(ctx, workspaceUUID, *blobUUID)
	if err != nil {
		return nil, err
	}
	if blob.Bucket != s.objects.Name() {
		return nil, errStorageUnavailable
	}
	return readBlob(ctx, s.objects, blob)
}

func readBlob(ctx context.Context, objects storage.ObjectStore, blob db.EventPayloadBlob) ([]byte, error) {
	if blob.Size < 0 || blob.Size > MaxBytes {
		return nil, errPayloadTooLarge
	}
	object, err := objects.Open(ctx, blob.Key, nil)
	if err != nil {
		return nil, fmt.Errorf("open event payload: %w", err)
	}
	defer object.Body.Close()
	if object.Size >= 0 && object.Size != blob.Size {
		return nil, errSizeMismatch
	}
	payload, err := io.ReadAll(io.LimitReader(object.Body, blob.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) != blob.Size {
		return nil, errSizeMismatch
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != blob.SHA256 {
		return nil, errDigestMismatch
	}
	return payload, nil
}
