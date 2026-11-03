package db

import "testing"

func TestEvaluationEvaluatorSetKeySortsVersionIDs(t *testing.T) {
	if got := EvaluationEvaluatorSetKey([]string{"eev_b", "eev_a", "eev_b"}); got != "eev_a|eev_b|eev_b" {
		t.Fatalf("key = %q", got)
	}
	if got := EvaluationEvaluatorSetKey(nil); got != "" {
		t.Fatalf("empty key = %q", got)
	}
}
