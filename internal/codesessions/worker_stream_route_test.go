package codesessions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPublishWorkerStreamPayloadsUsesProvidedRouteWithoutDatabase(t *testing.T) {
	service := newTestService(t, nil)
	sink := &recordingWorkerStreamSink{}
	service.SetPublicEventSink(sink)
	route := CodeSessionStreamRoute{
		CodeSessionID:     "cse-test",
		WorkspaceUUID:     "workspace-test",
		SessionExternalID: "session-test",
	}
	payloads := []json.RawMessage{json.RawMessage(`{"type":"stream_event"}`)}

	service.publishWorkerStreamPayloads(context.Background(), route, 7, payloads)

	if !sink.called || sink.route != route || sink.workerEpoch != 7 || len(sink.payloads) != 1 {
		t.Fatalf("stream publication = %#v, want supplied route and payload", sink)
	}
}

type recordingWorkerStreamSink struct {
	called      bool
	route       CodeSessionStreamRoute
	workerEpoch int64
	payloads    []json.RawMessage
}

func (s *recordingWorkerStreamSink) PublishCodeSessionEvents(context.Context, db.CodeSession, []json.RawMessage) error {
	return nil
}

func (s *recordingWorkerStreamSink) PublishCodeSessionStreamEvents(_ context.Context, route CodeSessionStreamRoute, workerEpoch int64, payloads []json.RawMessage) error {
	s.called = true
	s.route = route
	s.workerEpoch = workerEpoch
	s.payloads = payloads
	return nil
}
