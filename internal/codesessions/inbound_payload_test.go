package codesessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestLoadOffloadedPayloadRejectsMissingCorruptAndOversizedObjects(t *testing.T) {
	payload := []byte(`{"type":"user","text":"hydrate me"}`)
	digest := sha256.Sum256(payload)
	reference := &workerevents.PayloadReference{
		Key: "worker-inbound/test.json", Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]), CleanupJobID: "job_test",
	}
	tests := []struct {
		name      string
		store     *payloadTestStore
		reference workerevents.PayloadReference
	}{
		{name: "missing", store: &payloadTestStore{openErr: storage.ErrNotFound}, reference: *reference},
		{name: "object size mismatch", store: &payloadTestStore{data: payload, reportedSize: int64(len(payload) + 1)}, reference: *reference},
		{name: "content size mismatch", store: &payloadTestStore{data: payload[:len(payload)-1], reportedSize: -1}, reference: *reference},
		{name: "digest mismatch", store: &payloadTestStore{data: append([]byte(nil), payload...), reportedSize: int64(len(payload))}, reference: workerevents.PayloadReference{
			Key: reference.Key, Size: reference.Size, SHA256: "bad-digest", CleanupJobID: reference.CleanupJobID,
		}},
		{name: "oversized reference", store: &payloadTestStore{}, reference: workerevents.PayloadReference{
			Key: reference.Key, Size: maxIngressBodySize + 1, SHA256: reference.SHA256, CleanupJobID: reference.CleanupJobID,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{workerEventObjects: test.store}
			envelope := workerevents.EventEnvelope("cse_test", "csev_test", "payload_test", "user", "", nil, time.Time{})
			envelope.PayloadRef = &test.reference
			if _, err := service.loadOffloadedPayload(context.Background(), envelope); err == nil {
				t.Fatal("load corrupt offloaded payload succeeded")
			}
		})
	}
}

func TestLoadOffloadedPayloadRestoresExactPayload(t *testing.T) {
	payload := []byte(`{"type":"user","text":"hydrate me"}`)
	digest := sha256.Sum256(payload)
	service := &Service{workerEventObjects: &payloadTestStore{data: payload, reportedSize: int64(len(payload))}}
	envelope := workerevents.EventEnvelope("cse_test", "csev_test", "payload_test", "user", "", nil, time.Time{})
	envelope.PayloadRef = &workerevents.PayloadReference{
		Key: "worker-inbound/test.json", Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]), CleanupJobID: "job_test",
	}
	loaded, err := service.loadOffloadedPayload(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.Payload, payload) || loaded.PayloadRef == nil || loaded.PayloadRef.CleanupJobID != "job_test" {
		t.Fatalf("loaded envelope = %#v", loaded)
	}
	if workerevents.LargePayloadThreshold >= workerevents.MaxMessageBytes {
		t.Fatal("large payload threshold leaves no room for the reference envelope")
	}
}

type payloadTestStore struct {
	data         []byte
	reportedSize int64
	openErr      error
}

func (s *payloadTestStore) Ensure(context.Context) error { return nil }
func (s *payloadTestStore) Name() string                 { return "payload-test" }
func (s *payloadTestStore) Upload(context.Context, string, io.Reader, storage.UploadOptions) (storage.UploadResult, error) {
	return storage.UploadResult{}, errors.New("unexpected upload")
}
func (s *payloadTestStore) Open(context.Context, string, *storage.ByteRange) (storage.Object, error) {
	if s.openErr != nil {
		return storage.Object{}, s.openErr
	}
	return storage.Object{Body: io.NopCloser(bytes.NewReader(s.data)), Size: s.reportedSize}, nil
}
func (s *payloadTestStore) Copy(context.Context, string, string) (storage.CopyResult, error) {
	return storage.CopyResult{}, errors.New("unexpected copy")
}
func (s *payloadTestStore) Delete(context.Context, string, storage.DeleteOptions) error {
	return errors.New("unexpected delete")
}
