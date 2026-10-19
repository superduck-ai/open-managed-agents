package sessions

import (
	"encoding/json"
	"strings"
	"testing"
)

func userMessagePayload(t *testing.T, content string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(`{"type":"user.message","content":`+content+`}`), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestValidatePublicInputEventRejectsInvalidMessageMedia(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "invalid base64", content: `[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"%%%"}}]`, want: "valid base64"},
		{name: "unsupported image type", content: `[{"type":"image","source":{"type":"base64","media_type":"image/bmp","data":"AA=="}}]`, want: "not supported"},
		{name: "non pdf base64 document", content: `[{"type":"document","source":{"type":"base64","media_type":"text/plain","data":"AA=="}}]`, want: "application/pdf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePublicInputEvent("user.message", userMessagePayload(t, test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validatePublicInputEvent() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidatePublicInputEventAcceptsMessageMediaContract(t *testing.T) {
	content := `[
		{"type":"text","text":"Compare these"},
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}},
		{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}},
		{"type":"image","source":{"type":"file","file_id":"file_1"}},
		{"type":"document","title":"Spec","context":"v2","source":{"type":"base64","media_type":"application/pdf","data":"JVBERg=="}},
		{"type":"document","source":{"type":"text","media_type":"text/plain","data":"notes"}},
		{"type":"document","source":{"type":"url","url":"https://example.com/a.pdf"}},
		{"type":"document","source":{"type":"file","file_id":"file_2"}}
	]`
	if err := validatePublicInputEvent("user.message", userMessagePayload(t, content)); err != nil {
		t.Fatalf("validatePublicInputEvent() error = %v", err)
	}
}

func TestMessageMediaBlocksSkipsTextAndKeepsOrder(t *testing.T) {
	blocks, err := messageMediaBlocks(json.RawMessage(`{"content":[{"type":"text","text":"a"},{"type":"document","source":{"type":"file","file_id":"file_2"}},{"type":"image","source":{"type":"url","url":"u"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[0].Source.FileID != "file_2" || blocks[1].Source.URL != "u" {
		t.Fatalf("blocks = %+v", blocks)
	}
}
