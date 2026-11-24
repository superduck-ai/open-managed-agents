package fantasyadapter

import (
	"slices"
	"testing"

	"charm.land/fantasy"
)

func TestCollectFantasyStreamRejectsTruncatedToolTurn(t *testing.T) {
	stream := slices.Values([]fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolCall, ID: "toolu_test", ToolCallName: "add", ToolCallInput: "{}"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonLength},
	})
	message, err := collectFantasyStream(stream)
	if err == nil || message.Message.Call.ID != "" {
		t.Fatalf("got call %#v and error %v for truncated turn", message.Message.Call, err)
	}
}
