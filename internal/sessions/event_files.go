package sessions

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sessioneventfiles"
)

func normalizeInitialSessionEvents(
	session db.Session,
	raw json.RawMessage,
	bindings []sessioneventfiles.Binding,
	now time.Time,
) ([]db.SessionEvent, json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, session.OutcomeEvaluations, nil
	}
	var inputs []json.RawMessage
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, nil, errors.New("initial_events must be an array")
	}
	if len(inputs) == 0 || len(inputs) > 50 {
		return nil, nil, errors.New("initial_events must contain between 1 and 50 events")
	}
	events := make([]db.SessionEvent, 0, len(inputs))
	normalizedSession := session
	for index, input := range inputs {
		event, outcomes, changed, err := normalizeInputEvent(normalizedSession, input, now)
		if err != nil {
			return nil, nil, err
		}
		if err := validateInitialSessionEventOrder(events, event, index, len(inputs)); err != nil {
			return nil, nil, err
		}
		if err := sessioneventfiles.ValidateMountedReferences(event.EventType, event.Payload, bindings); err != nil {
			return nil, nil, err
		}
		if changed {
			normalizedSession.OutcomeEvaluations = outcomes
		}
		events = append(events, event)
	}
	return events, normalizedSession.OutcomeEvaluations, nil
}

func validateInitialSessionEventOrder(events []db.SessionEvent, event db.SessionEvent, index, total int) error {
	switch event.EventType {
	case "user.message", "user.define_outcome":
		return nil
	case "system.message":
		if index != total-1 {
			return errors.New("system.message must be the final initial event")
		}
		if len(events) == 0 || events[len(events)-1].EventType != "user.message" {
			return errors.New("system.message must immediately follow user.message")
		}
		return nil
	default:
		return errors.New("initial_events type must be user.message, user.define_outcome, or system.message")
	}
}
