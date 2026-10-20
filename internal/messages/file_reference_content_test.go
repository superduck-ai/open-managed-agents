package messages

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestMessageFileReferencesPreserveUnrelatedBytes(t *testing.T) {
	body := []byte(`{ "model":"m", "custom":{"file_id":"not-a-content-reference"},"messages":[{"role":"user","content":[{"type":"text","text":"file_id"}]}] }`)
	actual, err := resolveMessageFileReferences(body, func(string) (messageFileContent, *httpapi.Error) {
		t.Fatal("unrelated field caused a file lookup")
		return messageFileContent{}, nil
	})
	if err != nil || !bytes.Equal(actual, body) {
		t.Fatalf("non-file request changed: %v", err)
	}
}

func TestMessageFileContentTypesAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, block, mime, source string
		status                    int
	}{
		{"mime-mismatch", `{"type":"document","source":{"type":"file","file_id":"file_owned"}}`, "image/png", "", 400},
		{"malformed-file-id", `{"type":"document","source":{"type":"file","file_id":123}}`, "text/plain", "", 400},
		{"missing-file-id", `{"type":"document","source":{"type":"file"}}`, "text/plain", "", 400},
		{"bad-citation-config", `{"type":"document","citations":[],"source":{"type":"file","file_id":"file_owned"}}`, "text/plain", "", 400},
		{"plain", `{"type":"document","title":"title","context":"context","source":{"type":"file","file_id":"file_owned"}}`, "text/plain", "", 0},
		{"citations", `{"type":"document","citations":{"enabled":true},"source":{"type":"file","file_id":"file_owned"}}`, "text/plain", "text", 0},
		{"pdf", `{"type":"document","custom":"retained","source":{"type":"file","file_id":"file_owned"}}`, "application/pdf", "base64", 0},
		{"image", `{"type":"image","source":{"type":"file","file_id":"file_owned"}}`, "image/png", "base64", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte("actual source bytes")
			actual, _, err := resolveMessageFileContent([]byte("["+tc.block+"]"), func(id string) (messageFileContent, *httpapi.Error) {
				if id != "file_owned" {
					t.Fatalf("unexpected lookup%q", id)
				}
				return messageFileContent{mediaType: tc.mime, data: content}, nil
			})
			if tc.status != 0 {
				if err == nil || err.Status != tc.status {
					t.Fatalf("error%v, want%d", err, tc.status)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var blocks []struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Source struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			}
			if e := json.Unmarshal(actual, &blocks); e != nil || len(blocks) != 1 {
				t.Fatalf("invalid output:%s", actual)
			}
			if tc.source == "" {
				var doc messageFileText
				if e := json.Unmarshal([]byte(blocks[0].Text), &doc); e != nil || doc.Content != string(content) || doc.Title != "title" || doc.Context != "context" {
					t.Fatalf("document boundaries lost:%s", actual)
				}
			} else {
				expected := string(content)
				if tc.source == "base64" {
					expected = base64.StdEncoding.EncodeToString(content)
				}
				if blocks[0].Source.Type != tc.source || blocks[0].Source.MediaType != tc.mime || blocks[0].Source.Data != expected {
					t.Fatalf("source content changed:%s", actual)
				}
			}
			if strings.Contains(string(actual), "file_owned") {
				t.Fatal("local opaque ID forwarded upstream")
			}
		})
	}
}

func TestMessageFileNestedToolContentAndLookupFailure(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_owned","content":[{"type":"document","source":{"type":"file","file_id":"file_owned"}}]}]}]}`)
	result, err := resolveMessageFileReferences(body, func(string) (messageFileContent, *httpapi.Error) {
		return messageFileContent{mediaType: "text/plain", data: []byte("nested marker")}, nil
	})
	if err != nil || !strings.Contains(string(result), "nested marker") || !strings.Contains(string(result), "call_owned") || strings.Contains(string(result), "file_owned") {
		t.Fatalf("nested file transformation failed:%s %v", result, err)
	}
	_, err = resolveMessageFileReferences(body, func(string) (messageFileContent, *httpapi.Error) {
		return messageFileContent{}, fileReferenceNotFoundError()
	})
	if err == nil || err.Status != http.StatusNotFound {
		t.Fatalf("missing nested reference not rejected:%v", err)
	}
}

func TestMessageFileExpansionRemainsBounded(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"document","source":{"type":"file","file_id":"file_owned"}}]}]}`)
	_, err := resolveMessageFileReferences(body, func(string) (messageFileContent, *httpapi.Error) {
		return messageFileContent{mediaType: "text/plain", data: bytes.Repeat([]byte{0}, int(llmproviders.MaxMessagesRequestBodyBytes/6)+1024)}, nil
	})
	if err == nil || err.Status != 400 {
		t.Fatalf("expanded file request not rejected:%v", err)
	}
}
