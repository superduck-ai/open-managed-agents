package eventpayload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type readStore struct {
	storage.ObjectStore
	body []byte
	size int64
}

func (s readStore) Open(context.Context, string, *storage.ByteRange) (storage.Object, error) {
	return storage.Object{Body: io.NopCloser(bytes.NewReader(s.body)), Size: s.size}, nil
}
func TestReadBlobIntegrity(t *testing.T) {
	payload := []byte(`{"type":"test"}`)
	hash := sha256.Sum256(payload)
	blob := db.EventPayloadBlob{Size: int64(len(payload)), SHA256: hex.EncodeToString(hash[:])}
	for _, tc := range []struct {
		name string
		body []byte
		size int64
		want error
	}{
		{"truncated", payload[:3], -1, errSizeMismatch},
		{"oversized", append(append([]byte{}, payload...), 'x'), -1, errSizeMismatch},
		{"wrong digest", bytes.Repeat([]byte("x"), len(payload)), -1, errDigestMismatch},
		{"wrong reported size", payload, 1, errSizeMismatch},
		{"valid", payload, int64(len(payload)), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readBlob(context.Background(), readStore{body: tc.body, size: tc.size}, blob)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v, want %v", err, tc.want)
			}
			if err == nil && !bytes.Equal(got, payload) {
				t.Fatal("payload changed")
			}
		})
	}
}
func TestThresholdAndPreview(t *testing.T) {
	for _, size := range []int{Threshold - 1, Threshold, Threshold + 1} {
		if ExceedsThreshold(size) != (size > 32768) {
			t.Fatalf("unexpected decision for %d bytes", size)
		}
	}
	payload := []byte(strings.Repeat("a", 511) + "中tail")
	preview := Preview(payload)
	if len(preview) != 511 || !utf8.ValidString(preview) {
		t.Fatalf("preview split UTF-8: %q", preview)
	}
	summary, toolID, err := Summarize([]byte(`{"type":"x","name":"中文","tool_use_id":"","id":"fallback"}`), "x")
	if err != nil || toolID == nil || *toolID != "" || summary.Name == nil || *summary.Name != "中文" {
		t.Fatalf("metadata: %+v %v %v", summary, toolID, err)
	}
}
func TestInlineReferenceIsNotTrusted(t *testing.T) {
	store := New(nil, nil)
	payload := []byte(`{"blob_ref":{"version":1,"id":"epb_forged"}}`)
	got, err := store.restore(context.Background(), "workspace", payload, nil)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("inline payload changed: %s %v", got, err)
	}
	_, _, err = store.prepare(context.Background(), "org", "workspace", bytes.Repeat([]byte("x"), Threshold+1), Summary{})
	if !errors.Is(err, errStorageUnavailable) {
		t.Fatalf("missing store: %v", err)
	}
	got, ref, err := store.prepare(context.Background(), "org", "workspace", bytes.Repeat([]byte("x"), Threshold), Summary{})
	if err != nil || ref != nil || len(got) != Threshold {
		t.Fatalf("threshold must stay inline: %v", err)
	}
}

func TestOpaqueMetadataDoesNotRejectWorkerPayload(t *testing.T) {
	summary, _, err := Summarize([]byte(`{"type":"custom","name":{"nested":"value"},"id":123}`), "custom")
	if err != nil || summary.Name != nil {
		t.Fatalf("opaque metadata rejected: %+v %v", summary, err)
	}
}

func TestNonStringModelRequestStartIDDoesNotRejectPayload(t *testing.T) {
	summary, _, err := Summarize([]byte(`{"type":"span.model_request_end","model_request_start_id":123}`), "span.model_request_end")
	if err != nil || summary.ModelRequestStartID != "" {
		t.Fatalf("non-string start id must degrade to empty, got %q %v", summary.ModelRequestStartID, err)
	}
}

func TestSummaryCarriesBillingMetadata(t *testing.T) {
	payload := []byte(`{"type":"span.model_request_end","model":"claude","billing":{"list_cost":"900","currency":"USD"},"usage":{"input_tokens":10},"model_request_start_id":"sevt_start"}`)
	summary, _, err := Summarize(payload, "span.model_request_end")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if string(summary.Billing) != `{"list_cost":"900","currency":"USD"}` {
		t.Fatalf("billing not carried: %s", summary.Billing)
	}
	if string(summary.Usage) != `{"input_tokens":10}` || summary.ModelRequestStartID != "sevt_start" {
		t.Fatalf("usage/start link not carried: %s %q", summary.Usage, summary.ModelRequestStartID)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	billing, ok := back["billing"].(map[string]any)
	if !ok || billing["list_cost"] != "900" {
		t.Fatalf("summary json missing billing: %s", encoded)
	}
	if _, has := back["usage"]; !has {
		t.Fatalf("summary json missing usage: %s", encoded)
	}
}
