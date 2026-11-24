package harnessworker

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestHarnessContinuationRequiresDurableToolResult(t *testing.T) {
	tools := []db.HarnessTool{{UUID: "tool", CallID: "call", Name: "Read"}}
	if _, err := continuationFromTools("tool", tools); err == nil {
		t.Fatal("continued before the tool result was persisted")
	}
}

func TestHarnessContinuationPassesToolResultToNextStep(t *testing.T) {
	tools := []db.HarnessTool{
		{UUID: "tool-1", CallID: "call-1", Name: "Read", Result: new("first file")},
		{UUID: "tool-2", CallID: "call-2", Name: "Read", Result: new("second file missing"), Failed: true},
	}
	continuation, err := continuationFromTools("tool-2", tools)
	got := continuation.stepInput("session", "tool-2")
	want := cloudharness.ToolResult{CallID: "call-2", Text: "second file missing", Failed: true}
	if err != nil || continuation.ToolCount != 2 || got.DedupID != "tool-2:result" || got.Message.GroupID != "session" || got.Message.Content != want {
		t.Fatalf("continuation = %+v, %v", got, err)
	}
}
