package codesessions

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestApplyWorkerOutputEventsPublishesStreamPayloadsIndividuallyInOrder(t *testing.T) {
	service := newTestService(t, nil)
	sink := &recordingWorkerStreamSink{}
	service.SetPublicEventSink(sink)
	route := CodeSessionStreamRoute{
		CodeSessionID:     "cse-test",
		WorkspaceUUID:     "workspace-test",
		SessionExternalID: "session-test",
	}
	actions := []preparedWorkerOutputEvent{
		preparedStreamAction{payload: json.RawMessage(`{"sequence":1}`)},
		preparedStreamAction{payload: json.RawMessage(`{"sequence":2}`)},
	}

	if err := service.applyWorkerOutputEvents(context.Background(), route, 7, actions); err != nil {
		t.Fatalf("applyWorkerOutputEvents() error = %v", err)
	}

	if len(sink.publications) != 2 {
		t.Fatalf("stream publications = %#v, want two individual publications", sink.publications)
	}
	for index, publication := range sink.publications {
		if publication.route != route || publication.workerEpoch != 7 {
			t.Fatalf("stream publication %d = %#v, want supplied route", index, publication)
		}
		want := fmt.Sprintf(`{"sequence":%d}`, index+1)
		if string(publication.payload) != want {
			t.Fatalf("stream publication %d payload = %s, want %s", index, publication.payload, want)
		}
	}
}

type recordingWorkerStreamSink struct {
	publications []workerStreamPublication
}

type workerStreamPublication struct {
	route       CodeSessionStreamRoute
	workerEpoch int64
	payload     json.RawMessage
}

func (s *recordingWorkerStreamSink) PublishCodeSessionEvents(context.Context, db.CodeSession, []json.RawMessage) error {
	return nil
}

func (s *recordingWorkerStreamSink) PublishCodeSessionStreamEvent(_ context.Context, route CodeSessionStreamRoute, workerEpoch int64, payload json.RawMessage) error {
	s.publications = append(s.publications, workerStreamPublication{
		route:       route,
		workerEpoch: workerEpoch,
		payload:     append(json.RawMessage(nil), payload...),
	})
	return nil
}
