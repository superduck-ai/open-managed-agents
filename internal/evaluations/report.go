package evaluations

import "github.com/superduck-ai/open-managed-agents/internal/db"

type RoundSummary struct {
	Round      int
	Score      *float64
	PassRate   *float64
	Scored     int
	Passed     int
	Exceptions int
	Total      int
}

func SummarizeRound(items []db.EvaluationTaskItem, scores []db.EvaluationTaskScore, round int) RoundSummary {
	scoreByItem := map[string]db.EvaluationTaskScore{}
	for _, score := range scores {
		scoreByItem[score.ItemUUID] = score
	}
	summary := RoundSummary{Round: round}
	var total float64
	for _, item := range items {
		if item.Round != round {
			continue
		}
		summary.Total++
		score, ok := scoreByItem[item.UUID]
		if item.Status == itemFailed || (ok && score.Status != scoreCompleted) {
			summary.Exceptions++
			continue
		}
		if item.Status != itemCompleted || !ok {
			continue
		}
		summary.Scored++
		total += score.Score
		if score.Passed {
			summary.Passed++
		}
	}
	if summary.Scored > 0 {
		mean := total / float64(summary.Scored)
		rate := float64(summary.Passed) / float64(summary.Scored)
		summary.Score = &mean
		summary.PassRate = &rate
	}
	return summary
}

func latestRound(items []db.EvaluationTaskItem) int {
	round := 1
	for _, item := range items {
		if item.Round > round {
			round = item.Round
		}
	}
	return round
}

func roundSettled(items []db.EvaluationTaskItem, round int) bool {
	seen := false
	for _, item := range items {
		if item.Round != round {
			continue
		}
		seen = true
		if itemOpen(item.Status) {
			return false
		}
	}
	return seen
}
