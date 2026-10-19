package messages

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func staticAttachmentLookup(files map[string]attachmentContent) attachmentLookup {
	return func(_ context.Context, fileID string, limit int64) (attachmentContent, error) {
		content, ok := files[fileID]
		if !ok {
			return attachmentContent{}, nil
		}
		if int64(len(content.data)) > limit {
			return attachmentContent{}, errAttachmentsTooLarge
		}
		content.found = true
		return content, nil
	}
}

func TestExpandAttachmentSourcesPropagatesLookupFailure(t *testing.T) {
	failure := errors.New("database unavailable")
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file_1"}}]}]}`)
	_, err := expandAttachmentSources(context.Background(), body, func(context.Context, string, int64) (attachmentContent, error) {
		return attachmentContent{}, failure
	}, 1<<20)
	if !errors.Is(err, failure) {
		t.Fatalf("expandAttachmentSources() error = %v, want %v", err, failure)
	}
}

func TestExpandAttachmentSourcesRejectsBodiesOverLimit(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file_1"}}]}]}`)
	lookup := staticAttachmentLookup(map[string]attachmentContent{"file_1": {mediaType: "image/png", data: []byte(strings.Repeat("x", 512))}})
	_, err := expandAttachmentSources(context.Background(), body, lookup, int64(len(body))+64)
	if !errors.Is(err, errAttachmentsTooLarge) {
		t.Fatalf("expandAttachmentSources() error = %v, want too large", err)
	}
}

func TestExpandAttachmentSourcesLeavesRequestsWithoutFilesUntouched(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"z":1}`)
	expanded, err := expandAttachmentSources(context.Background(), body, staticAttachmentLookup(nil), 1<<20)
	if err != nil || string(expanded) != string(body) {
		t.Fatalf("expandAttachmentSources() = %s, %v", expanded, err)
	}
	urlBody := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://e/x.png","file_id":"ignored"}}]}]}`)
	expanded, err = expandAttachmentSources(context.Background(), urlBody, staticAttachmentLookup(nil), 1<<20)
	if err != nil || string(expanded) != string(urlBody) {
		t.Fatalf("url request changed: %s, %v", expanded, err)
	}
}

func TestExpandAttachmentSourcesRestoresProviderSources(t *testing.T) {
	body := []byte(`{"model":"m","stream":true,"messages":[
		{"role":"user","content":[
			{"type":"text","text":"look"},
			{"type":"image","cache_control":{"type":"ephemeral"},"source":{"type":"file","file_id":"file_png"}},
			{"type":"document","title":"Spec","source":{"type":"file","file_id":"file_pdf"}},
			{"type":"document","source":{"type":"file","file_id":"file_txt"}},
			{"type":"image","source":{"type":"file","file_id":"file_gone"}}
		]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"type":"file","file_id":"file_png"}}]}]}
	]}`)
	lookup := staticAttachmentLookup(map[string]attachmentContent{
		"file_png": {mediaType: "image/png", data: []byte{0}},
		"file_pdf": {mediaType: "application/pdf", data: []byte("%PDF")},
		"file_txt": {mediaType: "text/plain", data: []byte("notes")},
	})
	expanded, err := expandAttachmentSources(context.Background(), body, lookup, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(expanded, &request); err != nil {
		t.Fatal(err)
	}
	first := request.Messages[0].Content
	png := first[1]["source"].(map[string]any)
	if !request.Stream || png["type"] != "base64" || png["media_type"] != "image/png" || png["data"] != "AA==" || first[1]["cache_control"] == nil {
		t.Fatalf("image block = %v", first[1])
	}
	if pdf := first[2]["source"].(map[string]any); pdf["type"] != "base64" || pdf["media_type"] != "application/pdf" || first[2]["title"] != "Spec" {
		t.Fatalf("pdf block = %v", first[2])
	}
	if text := first[3]["source"].(map[string]any); text["type"] != "text" || text["data"] != "notes" {
		t.Fatalf("text document = %v", first[3])
	}
	if first[4]["type"] != "text" || !strings.Contains(first[4]["text"].(string), "no longer available") {
		t.Fatalf("missing attachment = %v", first[4])
	}
	nested := request.Messages[1].Content[0]["content"].([]any)[0].(map[string]any)["source"].(map[string]any)
	if nested["type"] != "base64" || nested["data"] != "AA==" {
		t.Fatalf("tool_result image source = %v", nested)
	}
	if strings.Contains(string(expanded), "file_id") {
		t.Fatalf("expanded request still references files: %s", expanded)
	}
}
