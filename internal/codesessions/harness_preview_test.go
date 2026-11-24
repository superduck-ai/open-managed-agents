package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type failingHarnessPreviewSink struct {
	recordingWorkerStreamSink
	failedSequence uint64
}

func (s *failingHarnessPreviewSink) PublishCodeSessionStreamEvent(ctx context.Context, route CodeSessionStreamRoute, epoch int64, payload json.RawMessage) error {
	if route.PreviewSequence == s.failedSequence {
		return errors.New("preview delivery unavailable")
	}
	return s.recordingWorkerStreamSink.PublishCodeSessionStreamEvent(ctx, route, epoch, payload)
}

func TestHarnessPreviewDropsTailAfterPublishFailure(t *testing.T) {
	service := newTestService(t, nil)
	sink := &failingHarnessPreviewSink{failedSequence: 3}
	service.SetPublicEventSink(sink)
	request := previewTestRequest()
	service.PublishHarnessModelText(t.Context(), request, "upstream", 0, "first")
	service.PublishHarnessModelText(t.Context(), request, "upstream", 0, " missing")
	service.PublishHarnessModelText(t.Context(), request, "upstream", 0, " tail")
	result := harnessTextResult(request, ModelRequestResult{ErrorType: "provider_error"})
	if len(sink.publications) != 2 || len(result.Messages) != 0 || !slices.Equal(result.EventIDs, []string{request.harnessMessageID()}) {
		t.Fatalf("publications=%d, failed result=%+v", len(sink.publications), result)
	}
	service.PublishHarnessModelText(t.Context(), request, "upstream", 0, "after completion")
	if len(sink.publications) != 2 {
		t.Fatal("published after the model request ended")
	}
}

func TestHarnessPreviewReconcilesTextBlocksAndThinking(t *testing.T) {
	service := newTestService(t, nil)
	sink := &recordingWorkerStreamSink{}
	service.SetPublicEventSink(sink)
	request := previewTestRequest()
	if err := service.StartModelThinking(t.Context(), request, "upstream", 0); err != nil {
		t.Fatal(err)
	}
	service.PublishHarnessModelText(t.Context(), request, "upstream", 3, "hello")
	service.PublishHarnessModelText(t.Context(), request, "upstream", 3, " world")
	service.PublishHarnessModelText(t.Context(), request, "upstream", 2, "second block")
	if len(sink.publications) != 5 {
		t.Fatalf("publications = %d, want 5", len(sink.publications))
	}
	textID := request.harnessMessageID()
	thinkingBlock := maevents.StableAssistantEventID(request.CodeSessionID, "upstream", 0, "agent.thinking")
	thinkingID := request.harnessThinkingID(thinkingBlock)
	assertHarnessPreviewPayload(t, sink.publications[0], request, 0, modelPreviewStart{
		Type: "event_start", Event: modelPreviewTarget{Type: "agent.thinking", ID: thinkingID},
	})
	assertHarnessPreviewPayload(t, sink.publications[1], request, 1, modelPreviewStart{
		Type: "event_start", Event: modelPreviewTarget{Type: "agent.message", ID: textID},
	})
	for i, text := range []string{"hello", " world", "second block"} {
		index := 0
		if i == 2 {
			index = 1
		}
		assertHarnessPreviewPayload(t, sink.publications[i+2], request, uint64(i+2), modelPreviewDelta{
			Type: "event_delta", EventID: textID,
			Delta: modelPreviewContentDelta{Type: "content_delta", Index: index, Content: ModelRequestContent{Type: "text", Text: &text}},
		})
	}
	result := harnessTextResult(request, ModelRequestResult{Messages: []ModelRequestMessage{
		{ID: thinkingBlock, Type: "agent.thinking", Content: []ModelRequestContent{{Type: "thinking", Text: new("private")}}},
		{ID: maevents.StableAssistantEventID(request.CodeSessionID, "upstream", 2, "agent.message"), Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: new("second block")}}},
		{ID: maevents.StableAssistantEventID(request.CodeSessionID, "upstream", 3, "agent.message"), Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: new("hello world")}}},
	}})
	if result.ErrorType != "" || !slices.Equal(result.EventIDs, []string{thinkingID, textID}) ||
		len(result.Messages) != 2 || len(result.Messages[0].Content) != 0 || len(result.Messages[1].Content) != 2 ||
		*result.Messages[1].Content[0].Text != "hello world" || *result.Messages[1].Content[1].Text != "second block" {
		t.Fatalf("buffered result = %+v", result)
	}
}

func previewTestRequest() *ModelRequest {
	return &ModelRequest{StartID: "request", CodeSessionID: "code", harnessLease: &db.HarnessTurnLease{Epoch: 7},
		codeSession: db.CodeSession{WorkspaceUUID: "workspace", SessionExternalID: "session"}}
}

func assertHarnessPreviewPayload(t *testing.T, publication workerStreamPublication, request *ModelRequest, sequence uint64, payload any) {
	t.Helper()
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if publication.route.ModelRequestID != request.StartID || publication.route.PreviewSequence != sequence ||
		publication.workerEpoch != request.harnessLease.Epoch || string(publication.payload) != string(want) {
		t.Fatalf("preview=%+v %s, want sequence %d and payload %s", publication.route, publication.payload, sequence, want)
	}
}
