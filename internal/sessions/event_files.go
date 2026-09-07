package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/open-managed-agents/internal/sessioneventfiles"
)

func prepareEventWorkerContent(
	event db.SessionEvent,
	bindings []sessioncontract.EventFileBinding,
) (json.RawMessage, error) {
	payload, err := sessioneventfiles.WorkerPayload(event.EventType, event.Payload, bindings)
	if err == nil {
		return payload, nil
	}
	if sessioneventfiles.IsValidationError(err) {
		return nil, markEventInputError(err)
	}
	return nil, err
}
