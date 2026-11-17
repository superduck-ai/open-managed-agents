package evaluations

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestParseCompletionRejectsEmptyReplies(t *testing.T) {
	for _, testCase := range []struct {
		backend string
		payload string
	}{
		{llmproviders.Messages, `{"content":[{"type":"thinking","text":"x"}]}`},
		{llmproviders.Messages, `not json`},
		{llmproviders.ChatCompletions, `{"choices":[]}`},
	} {
		if _, err := parseCompletion(testCase.backend, []byte(testCase.payload)); !errors.Is(err, errModelFailed) {
			t.Fatalf("%s %s err = %v", testCase.backend, testCase.payload, err)
		}
	}
}

func TestParseCompletionReadsUsage(t *testing.T) {
	messages, err := parseCompletion(llmproviders.Messages, []byte(`{"content":[{"type":"text","text":"{\"score\":80}"}],"usage":{"input_tokens":900,"output_tokens":40}}`))
	if err != nil || messages.Text != `{"score":80}` || messages.InputTokens != 900 || messages.OutputTokens != 40 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	chat, err := parseCompletion(llmproviders.ChatCompletions, []byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":700,"completion_tokens":25}}`))
	if err != nil || chat.Text != "ok" || chat.InputTokens != 700 || chat.OutputTokens != 25 {
		t.Fatalf("chat = %+v err=%v", chat, err)
	}
}
