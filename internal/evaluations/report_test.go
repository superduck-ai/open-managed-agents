package evaluations

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSummarizeRoundCountsOnlySessionAndScoreFailures(t *testing.T) {
	items := []db.EvaluationTaskItem{
		{UUID: "passed", Round: 1, Status: itemCompleted},
		{UUID: "missed", Round: 1, Status: itemCompleted},
		{UUID: "failed-session", Round: 1, Status: itemFailed},
		{UUID: "failed-score", Round: 1, Status: itemCompleted},
		{UUID: "cancelled", Round: 1, Status: itemCancelled},
		{UUID: "running", Round: 1, Status: itemRunning},
	}
	scores := []db.EvaluationTaskScore{
		{ItemUUID: "passed", Status: scoreCompleted, Score: 80, Passed: true},
		{ItemUUID: "failed-score", Status: scoreFailed},
	}
	summary := SummarizeRound(items, scores, 1)
	if summary.Total != 6 || summary.Scored != 1 || summary.Passed != 1 || summary.Exceptions != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Score == nil || *summary.Score != 80 || summary.PassRate == nil || *summary.PassRate != 1 {
		t.Fatalf("rates = %+v", summary)
	}
}
