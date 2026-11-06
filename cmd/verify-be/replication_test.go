package main

import (
	"io"
	"strings"
	"testing"
)

func TestReplicationRejectsMissingEvidence(t *testing.T) {
	selected := scenarios["chat.replication"]
	input := `{"Package":"` + selected.Package + `","Test":"` + selected.Test + `","Action":"pass"}` + "\n"
	result, err := evaluate(strings.NewReader(input), 0, selected)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == "pass" || len(result.MissingStages) != 4 {
		t.Fatalf("missing replication proofs accepted: %+v", result)
	}
}

func TestReplicationScenarioUsesRealBackendWithoutSandbox(t *testing.T) {
	options, err := parseCLI([]string{"chat", "replication"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	selected := scenarios[options.Scenario]
	if options.Scenario != "chat.replication" || needsWorker(options.Scenario) || selected.DependenciesOnly || len(selected.Stages) != 4 {
		t.Fatalf("invalid replication scenario: %+v %+v", options, selected)
	}
}

func TestReplicationReportStatesComponentCoverage(t *testing.T) {
	var out strings.Builder
	writeReportCoverage(&out, report{Scenario: "chat.replication"})
	text := out.String()
	for _, phrase := range []string{"real standalone backend and PostgreSQL", "No model Worker", "actual 413"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("replication scope missing %q: %s", phrase, text)
		}
	}
}
