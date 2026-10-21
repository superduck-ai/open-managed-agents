package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type fixtureOverlay struct {
	Replace map[string]string `json:"Replace"`
}

func (e *environment) baselineFixtureOverlay(selected scenario) (string, error) {
	if e.backendCommit == "" || selected.Test != "TestChatPerformance" {
		return "", nil
	}
	overlay := fixtureOverlay{Replace: make(map[string]string)}
	for _, name := range []string{"session_mapper", "session_thread_mapper", "session_resource_file_mapper"} {
		filename := name + ".sqlmap.gen.go"
		reference := filepath.Join(e.directory, "backend-source", "internal", "db", filename)
		info, err := os.Stat(reference)
		if err != nil {
			return "", fmt.Errorf("baseline fixture mapper %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("baseline fixture mapper %s must be a regular file", name)
		}
		overlay.Replace[filepath.Join(e.root, "internal", "db", filename)] = reference
	}
	path := filepath.Join(e.directory, "baseline-fixture-overlay.json")
	return path, writeJSON(path, overlay)
}
