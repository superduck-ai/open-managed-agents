package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestThreadMetricsPreserveSeparateInitializationBoundaries(t *testing.T) {
	thread := db.SessionThread{ExternalID: "fresh", Stats: json.RawMessage(`{}`), Usage: json.RawMessage(`{"input_tokens":5}`)}
	got := responseFromThread(thread)
	if string(got.Stats) != "null" || string(got.Usage) != "null" {
		t.Fatalf("cold metrics must be null, got %s %s", got.Stats, got.Usage)
	}
	thread.StatsInitialized = true
	got = responseFromThread(thread)
	if string(got.Stats) != "{}" || string(got.Usage) != "null" {
		t.Fatalf("running must expose stats only: %s %s", got.Stats, got.Usage)
	}
	thread.UsageInitialized = true
	got = responseFromThread(thread)
	if string(got.Usage) != string(thread.Usage) || string(got.Stats) != string(thread.Stats) {
		t.Fatal("initialized raw metrics changed")
	}
	thread.Status = "terminated"
	if string(responseFromThread(thread).Usage) != string(thread.Usage) {
		t.Fatal("termination reset usage")
	}
	thread.UsageInitialized = false
	if string(responseFromThread(thread).Usage) != "null" {
		t.Fatal("termination was treated as idle")
	}
}
