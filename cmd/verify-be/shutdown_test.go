package main

import (
	"io"
	"testing"
)

func TestShutdownScenarioDoesNotRequireWorker(t *testing.T) {
	options, err := parseCLI([]string{"chat", "shutdown"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.Scenario != "chat.shutdown" || needsWorker(options.Scenario) {
		t.Fatalf("shutdown options require a model Worker: %+v", options)
	}
}
