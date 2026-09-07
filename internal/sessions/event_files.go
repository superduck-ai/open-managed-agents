package sessions

import (
	jsonv1 "encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncreation"
	"github.com/superduck-ai/open-managed-agents/internal/sessioneventfiles"
)

func prepareEventWorkerContent(
	event db.SessionEvent,
	bindings []sessioncontract.EventFileBinding,
) (jsonv1.RawMessage, error) {
	payload, err := sessioneventfiles.WorkerPayload(event.EventType, event.Payload, bindings)
	if err == nil {
		return payload, nil
	}
	if sessioneventfiles.IsValidationError(err) {
		return nil, markEventInputError(err)
	}
	return nil, err
}

func normalizeInitialSessionEvents(
	session db.Session,
	raw jsonv1.RawMessage,
	bindings []sessioncontract.EventFileBinding,
	now time.Time,
) ([]db.SessionEvent, jsonv1.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, session.OutcomeEvaluations, nil
	}
	inputs, err := sessioncreation.ParseInitialEvents(raw)
	if err != nil {
		return nil, nil, markEventInputError(err)
	}
	events := make([]db.SessionEvent, 0, len(inputs))
	normalizedSession := session
	for _, input := range inputs {
		event, outcomes, changed, err := normalizeInputEvent(normalizedSession, input.Raw, now)
		if err != nil {
			return nil, nil, err
		}
		if event.ThreadExternalID != nil {
			return nil, nil, markEventInputError(errors.New("initial_events must not include session_thread_id"))
		}
		if err := sessioneventfiles.ValidateMountedReferences(event.EventType, event.Payload, bindings); err != nil {
			return nil, nil, markEventInputError(err)
		}
		if changed {
			normalizedSession.OutcomeEvaluations = outcomes
		}
		events = append(events, event)
	}
	return events, normalizedSession.OutcomeEvaluations, nil
}
