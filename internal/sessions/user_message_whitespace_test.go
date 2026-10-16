package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestUserMessageRejectsWhitespaceText(t *testing.T) {
	for _, text := range []string{"", " ", "\t\r\n", "\u00a0\u3000", "\u2003"} {
		raw, err := json.Marshal(map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": text}}})
		if err != nil {
			t.Fatal(err)
		}
		event, _, _, err := normalizeInputEvent(db.Session{}, raw, time.Now())
		if err == nil || event.ExternalID != "" {
			t.Fatalf("blank text accepted: %q", text)
		}
		session := db.Session{}
		events, err := normalizeSessionInitialEvents(&session, json.RawMessage("["+string(raw)+"]"), time.Now())
		if err == nil || events != nil {
			t.Fatalf("blank initial event accepted: %q", text)
		}
	}
	payload := map[string]any{"content": []any{map[string]any{"type": "text", "text": "valid"}, map[string]any{"type": "text", "text": " \t"}}}
	if validatePublicInputEvent("user.message", payload) == nil {
		t.Fatal("blank block hidden behind valid block")
	}
}

func TestUserMessagePreservesNonblankText(t *testing.T) {
	for _, text := range []string{" x ", "\n中文\t", " \u200b "} {
		raw, err := json.Marshal(map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": text}}})
		if err != nil {
			t.Fatal(err)
		}
		event, _, _, err := normalizeInputEvent(db.Session{}, raw, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err = json.Unmarshal(event.Payload, &decoded); err != nil {
			t.Fatal(err)
		}
		got := decoded["content"].([]any)[0].(map[string]any)["text"]
		if got != text {
			t.Fatalf("text changed: got %q want %q", got, text)
		}
	}
	for _, typ := range []string{"user.tool_result", "user.custom_tool_result"} {
		payload := map[string]any{"tool_use_id": "tool-1", "custom_tool_use_id": "tool-1", "content": []any{map[string]any{"type": "text", "text": " \t\n"}}}
		if err := validatePublicInputEvent(typ, payload); err != nil {
			t.Fatalf("tool output affected: %v", err)
		}
	}
	payload := map[string]any{"content": []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "eA=="}}}}
	if err := validatePublicInputEvent("user.message", payload); err != nil {
		t.Fatal(err)
	}
}
