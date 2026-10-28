package eventpayload

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const TraceContextKey = "_trace_context"

type storedTraceContext struct {
	TraceParent string `json:"traceparent"`
	TraceState  string `json:"tracestate,omitempty"`
}

func storeTraceContext(event *db.SessionEvent) error {
	if event.EventType != "user.message" || event.TraceParent == nil {
		return nil
	}
	var stored map[string]jsontext.Value
	if err := jsonv2.Unmarshal(event.Payload, &stored); err != nil {
		return err
	}
	trace := storedTraceContext{TraceParent: *event.TraceParent}
	if event.TraceState != nil {
		trace.TraceState = *event.TraceState
	}
	encoded, err := jsonv2.Marshal(trace)
	if err != nil {
		return err
	}
	stored[TraceContextKey] = encoded
	event.Payload, err = jsonv2.Marshal(stored)
	return err
}

func liftTraceContext(event *db.SessionEvent) error {
	if !bytes.Contains(event.Payload, []byte(`"`+TraceContextKey+`"`)) {
		return nil
	}
	var stored map[string]jsontext.Value
	if err := jsonv2.Unmarshal(event.Payload, &stored); err != nil {
		return err
	}
	raw, found := stored[TraceContextKey]
	if !found {
		return nil
	}
	var trace storedTraceContext
	if err := jsonv2.Unmarshal(raw, &trace); err != nil {
		return err
	}
	if trace.TraceParent != "" {
		event.TraceParent = &trace.TraceParent
		if trace.TraceState != "" {
			event.TraceState = &trace.TraceState
		}
	}
	delete(stored, TraceContextKey)
	payload, err := jsonv2.Marshal(stored)
	if err != nil {
		return err
	}
	event.Payload = payload
	return nil
}
