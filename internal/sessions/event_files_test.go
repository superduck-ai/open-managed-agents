package sessions

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestInitialSessionEventsUseSharedValidation(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"user.message","content":[{"type":"text","text":"hello"}]},{"type":"system.message","content":[{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}]`,
		`[{"type":"user.message","content":[{"type":"text"}]}]`,
		`[{"type":"user.define_outcome","description":"done","rubric":"pass"}]`,
		`[{"type":"system.message","content":[{"type":"text","text":"context"}]}]`,
	} {
		_, _, err := normalizeInitialSessionEvents(db.Session{}, json.RawMessage(raw), nil, time.Now())
		if err == nil || !isEventInputError(err) {
			t.Fatalf("initial events %s: error = %v, want input error", raw, err)
		}
	}

	raw := json.RawMessage(`[{"type":"user.message","content":[{"type":"image","filename":"diagram.png","source":{"type":"url","url":"https://example.com/image.png"}}]},{"type":"system.message","content":[{"type":"text","text":"context"}]}]`)
	events, _, err := normalizeInitialSessionEvents(db.Session{}, raw, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || !bytes.Contains(events[0].Payload, []byte(`"filename":"diagram.png"`)) {
		t.Fatalf("initial events lost original content: %+v", events)
	}
}

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

func TestNormalizeInputEventRejectsMissingFileIDAtInputBoundary(t *testing.T) {
	_, _, _, err := normalizeInputEvent(
		db.Session{},
		json.RawMessage(`{"type":"user.message","content":[{"type":"document","source":{"type":"file"}}]}`),
		time.Now().UTC(),
	)
	if err == nil || !isEventInputError(err) {
		t.Fatalf("normalizeInputEvent() error = %v, want eventInputError", err)
	}
}
