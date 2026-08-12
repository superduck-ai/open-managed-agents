package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestNormalizeInputEventClassifiesInputAndInternalErrors(t *testing.T) {
	t.Run("input error", func(t *testing.T) {
		_, _, _, err := normalizeInputEvent(
			db.Session{},
			json.RawMessage(`{"type":"user.message","content":[]}`),
			time.Now().UTC(),
		)
		if err == nil || !isEventInputError(err) {
			t.Fatalf("normalizeInputEvent() error = %v, want eventInputError", err)
		}
	})

	t.Run("stored state error", func(t *testing.T) {
		_, _, _, err := normalizeInputEvent(
			db.Session{OutcomeEvaluations: json.RawMessage(`{"invalid":true}`)},
			json.RawMessage(`{
				"type":"user.define_outcome",
				"description":"done",
				"rubric":{"type":"text","text":"must pass"}
			}`),
			time.Now().UTC(),
		)
		if err == nil || isEventInputError(err) {
			t.Fatalf("normalizeInputEvent() error = %v, want internal processing error", err)
		}
	})
}
