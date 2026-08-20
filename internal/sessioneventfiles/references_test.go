package sessioneventfiles

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestReferenceValidationFailures(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		bindings  []sessioncontract.EventFileBinding
		want      string
	}{
		{
			name:      "file is not attached",
			eventType: "user.message",
			payload:   `{"type":"user.message","content":[{"type":"document","source":{"type":"file","file_id":"file_missing"}}]}`,
			want:      "Session Resources API",
		},
		{
			name:      "image mime type mismatch",
			eventType: "user.message",
			payload:   `{"type":"user.message","content":[{"type":"image","source":{"type":"file","file_id":"file_text"}}]}`,
			bindings: []sessioncontract.EventFileBinding{{
				FileID: "file_text", Path: "/uploads/not-image.txt", MimeType: "text/plain",
			}},
			want: "not a supported image",
		},
		{
			name:      "client local path",
			eventType: "user.message",
			payload:   `{"type":"user.message","content":[{"type":"document","source":{"type":"path","path":"/tmp/private.txt"}}]}`,
			want:      "local file paths are not accepted",
		},
		{
			name:      "file ID is all whitespace",
			eventType: "user.message",
			payload:   `{"type":"user.message","content":[{"type":"document","source":{"type":"file","file_id":"   "}}]}`,
			want:      "Session Resources API",
		},
		{
			name:      "padded file ID uses exact match",
			eventType: "user.message",
			payload:   `{"type":"user.message","content":[{"type":"document","source":{"type":"file","file_id":" file_doc "}}]}`,
			bindings:  []sessioncontract.EventFileBinding{{FileID: "file_doc", Path: "/uploads/doc.pdf", MimeType: "application/pdf"}},
			want:      "Session Resources API",
		},
		{
			name:      "file block outside user message",
			eventType: "system.message",
			payload:   `{"type":"system.message","content":[{"type":"document","source":{"type":"file","file_id":"file_doc"}}]}`,
			bindings:  []sessioncontract.EventFileBinding{{FileID: "file_doc", Path: "/uploads/doc.pdf", MimeType: "application/pdf"}},
			want:      "only supported in user.message",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(test.payload)
			mountedErr := ValidateMountedReferences(test.eventType, raw, test.bindings)
			if mountedErr == nil || !strings.Contains(mountedErr.Error(), test.want) {
				t.Fatalf("ValidateMountedReferences() error = %v, want containing %q", mountedErr, test.want)
			}
			if !IsValidationError(mountedErr) {
				t.Fatalf("ValidateMountedReferences() error type = %T, want validation error", mountedErr)
			}
		})
	}
}

func TestValidateMountedReferencesRejectsLocalSourceTypes(t *testing.T) {
	for _, sourceType := range []string{"path", "file_path", "local_file", "local_path"} {
		t.Run(sourceType, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"user.message","content":[{"type":"document","source":{"type":` +
				strconv.Quote(sourceType) + `,"file_path":"/tmp/private.txt"}}]}`)
			err := ValidateMountedReferences("user.message", raw, nil)
			if err == nil || !strings.Contains(err.Error(), "local file paths are not accepted") {
				t.Fatalf("ValidateMountedReferences() error = %v, want local path rejection", err)
			}
		})
	}
}

func TestWorkerPayloadReturnsReferenceValidationError(t *testing.T) {
	raw := json.RawMessage(`{"type":"user.message","content":[{"type":"document","source":{"type":"file","file_id":"file_missing"}}]}`)
	worker, err := WorkerPayload("user.message", raw, nil)
	if err == nil || !IsValidationError(err) {
		t.Fatalf("WorkerPayload() error = %v, want validation error", err)
	}
	if worker != nil {
		t.Fatalf("WorkerPayload() payload = %s, want nil", worker)
	}
}

func TestWorkerPayloadInjectsDeduplicatedMountedPaths(t *testing.T) {
	publicPayload := json.RawMessage(`{
		"type":"user.message",
		"content":[
			{"type":"text","text":"summarize both files"},
			{"type":"document","source":{"type":"file","file_id":"file_doc"}},
			{"type":"document","source":{"type":"file","file_id":"file_doc"}},
			{"type":"image","source":{"type":"file","file_id":"file_image"}}
		]
	}`)
	workerPayload, err := WorkerPayload("user.message", publicPayload, []sessioncontract.EventFileBinding{
		{FileID: "file_doc", Path: `/uploads/reference "document".pdf`, MimeType: "application/pdf"},
		{FileID: "file_image", Path: "/uploads/chart image.png", MimeType: "image/png"},
	})
	if err != nil {
		t.Fatalf("WorkerPayload() error = %v", err)
	}
	if !bytes.Contains(publicPayload, []byte(`"file_id":"file_doc"`)) {
		t.Fatalf("public payload was unexpectedly changed: %s", publicPayload)
	}
	if bytes.Contains(workerPayload, []byte(`"file_id"`)) || bytes.Contains(workerPayload, []byte(`"source":{"type":"file"`)) {
		t.Fatalf("worker payload still contains file-source blocks: %s", workerPayload)
	}

	var envelope struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(workerPayload, &envelope); err != nil {
		t.Fatalf("decode worker payload: %v", err)
	}
	if len(envelope.Content) != 2 {
		t.Fatalf("worker content count = %d, want original text plus mentions", len(envelope.Content))
	}
	var mention struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(envelope.Content[1], &mention); err != nil {
		t.Fatalf("decode mention block: %v", err)
	}
	for _, expected := range []string{
		`@"/mnt/session/uploads/reference \"document\".pdf"`,
		`@"/mnt/session/uploads/chart image.png"`,
	} {
		if !strings.Contains(mention.Text, expected) {
			t.Fatalf("mention text %q does not contain %q", mention.Text, expected)
		}
	}
	if strings.Count(mention.Text, "reference") != 1 {
		t.Fatalf("duplicate file reference was not removed: %q", mention.Text)
	}
}

func TestWorkerPayloadLeavesPlainTextUnchanged(t *testing.T) {
	raw := json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hello"}]}`)
	worker, err := WorkerPayload("user.message", raw, nil)
	if err != nil {
		t.Fatalf("WorkerPayload() error = %v", err)
	}
	if !bytes.Equal(worker, raw) {
		t.Fatalf("WorkerPayload() = %s, want unchanged %s", worker, raw)
	}
}
