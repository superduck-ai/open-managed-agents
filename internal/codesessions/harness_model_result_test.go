package codesessions

import "testing"

func TestHarnessModelResultKeepsOnlyRestorableText(t *testing.T) {
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test"}
	redacted := ModelRequestMessage{ID: "redacted", Type: "agent.message", Content: []ModelRequestContent{{Type: "redacted"}}}
	nontext := harnessTextResult(request, ModelRequestResult{Messages: []ModelRequestMessage{redacted}})
	if nontext.ErrorType != "invalid_response" || len(nontext.Messages) != 0 {
		t.Fatalf("non-text generation was accepted: %+v", nontext)
	}
	message := ModelRequestMessage{ID: "reply", Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: new("hello")}}}
	result := harnessTextResult(request, ModelRequestResult{Messages: []ModelRequestMessage{redacted, message}})
	if result.ErrorType != "" || len(result.Messages) != 1 || len(result.Messages[0].Content) != 1 ||
		len(result.EventIDs) != 1 || result.EventIDs[0] != request.harnessMessageID() || result.Messages[0].Content[0].Text == nil ||
		*result.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("unexpected harness reply: %+v", result)
	}
}

func TestHarnessModelResultRejectsAmbiguousToolCall(t *testing.T) {
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test"}
	for _, input := range []ModelRequestResult{
		{ToolUseIDs: []string{"one"}, ToolUses: []ModelRequestToolUse{{ID: "two", Name: "Read", Input: []byte(`{}`)}}},
		{ToolUseIDs: []string{"one", "two"}, ToolUses: []ModelRequestToolUse{{ID: "one"}, {ID: "two"}}},
	} {
		result := harnessTextResult(request, input)
		if result.ErrorType != "invalid_response" || len(result.ToolUses) != 0 {
			t.Fatalf("ambiguous call was accepted: %+v", result)
		}
	}
}

func TestHarnessModelResultKeepsToolOnlyCompletion(t *testing.T) {
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test"}
	result := harnessTextResult(request, ModelRequestResult{
		ToolUseIDs: []string{"one"},
		ToolUses:   []ModelRequestToolUse{{ID: "one", Name: "Read", Input: []byte(`{"file_path":"/tmp/example"}`)}},
	})
	if result.ErrorType != "" || len(result.ToolUses) != 1 || len(result.Messages) != 0 {
		t.Fatalf("unexpected tool-only result: %+v", result)
	}
}
