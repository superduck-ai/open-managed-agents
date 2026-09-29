package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluateRejectsIncompleteRuns(t *testing.T) {
	selected := scenarios["chat.roundtrip"]
	for _, tc := range []struct {
		name   string
		events []testEvent
		code   int
		proofs bool
	}{
		{name: "no tests", proofs: true},
		{name: "package only", events: []testEvent{{Action: "pass"}}, proofs: true},
		{name: "skipped", events: []testEvent{{Test: selected.Test, Action: "skip"}, {Action: "pass"}}, proofs: true},
		{name: "missing proof", events: []testEvent{{Test: selected.Test, Action: "pass"}, {Action: "pass"}}},
		{name: "nonzero exit", events: []testEvent{{Test: selected.Test, Action: "pass"}, {Action: "pass"}}, proofs: true, code: 1},
		{name: "child skipped", events: []testEvent{{Test: selected.Test + "/child", Action: "skip"}, {Test: selected.Test, Action: "pass"}, {Action: "pass"}}, proofs: true},
		{name: "failure then pass", events: []testEvent{{Test: selected.Test, Action: "fail"}, {Test: selected.Test, Action: "pass"}, {Action: "pass"}}, proofs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := evaluate(strings.NewReader(testOutput(t, selected, tc.events, tc.proofs)), tc.code, selected)
			if err != nil || result.Status != "fail" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	if _, err := evaluate(strings.NewReader("not JSON"), 0, selected); err == nil {
		t.Fatal("malformed evidence must fail")
	}
}

func TestEvaluateRequiresSelectedScenario(t *testing.T) {
	roundtrip, tools := scenarios["chat.roundtrip"], scenarios["chat.tools"]
	output := testOutput(t, roundtrip, []testEvent{{Test: roundtrip.Test, Action: "pass"}, {Action: "pass"}}, true)
	result, err := evaluate(strings.NewReader(output), 0, tools)
	if err != nil || result.Status != "fail" {
		t.Fatalf("wrong scenario accepted: %+v %v", result, err)
	}
}

func TestEvaluateCompletedScenario(t *testing.T) {
	for name, selected := range scenarios {
		t.Run(name, func(t *testing.T) {
			output := testOutput(t, selected, []testEvent{{Test: selected.Test, Action: "pass"}, {Action: "pass"}}, true)
			result, err := evaluate(strings.NewReader(output), 0, selected)
			if err != nil || result.Status != "pass" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func testOutput(t *testing.T, selected scenario, events []testEvent, proofs bool) string {
	t.Helper()
	if proofs {
		for _, stage := range selected.Stages {
			data, _ := json.Marshal(proof{Stage: stage, ElapsedMS: 1})
			events = append(events, testEvent{Test: selected.Test, Action: "output", Output: "CHAT_PROOF " + string(data)})
		}
	}
	var output strings.Builder
	for _, event := range events {
		event.Package = selected.Package
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(data)
		output.WriteByte('\n')
	}
	return output.String()
}

func TestEvaluatePackageAndTimeout(t *testing.T) {
	selected := scenarios["chat.roundtrip"]
	moved := selected
	moved.Package = "example.test/another/package"
	output := testOutput(t, moved, []testEvent{{Test: moved.Test, Action: "pass"}, {Action: "pass"}}, true)
	if result, err := evaluate(strings.NewReader(output), 0, selected); err != nil || result.Status != "fail" {
		t.Fatalf("foreign package accepted %+v %v", result, err)
	}
	if result, err := evaluate(strings.NewReader(output), 0, moved); err != nil || result.Status != "pass" {
		t.Fatalf("configured package ignored %+v %v", result, err)
	}
	for _, message := range []string{"panic: test timed out after 1m", "CHAT_TIMEOUT waiting for final"} {
		output := testOutput(t, selected, []testEvent{{Action: "output", Output: message}, {Action: "fail"}}, false)
		if result, err := evaluate(strings.NewReader(output), 1, selected); err != nil || result.FailureKind != "scenario_timeout" {
			t.Fatalf("wrong timeout verdict %+v %v", result, err)
		}
	}
}
