package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type fixtureOverlay struct {
	Replace map[string]string `json:"Replace"`
}

func (e *environment) baselineFixtureOverlay(selected scenario) (string, error) {
	if e.backendCommit == "" || selected.Test != "TestChatPerformance" {
		return "", nil
	}
	overlay := fixtureOverlay{Replace: make(map[string]string)}
	for _, mapper := range []struct{ name, function string }{
		{"session_mapper", ""},
		{"session_thread_mapper", ""},
		{"session_resource_file_mapper", ""},
		{"session_resource_mapper", "buildSessionResourceMapperInsertDirectory"},
	} {
		name := mapper.name
		filename := name + ".sqlmap.gen.go"
		reference := filepath.Join(e.directory, "backend-source", "internal", "db", filename)
		info, err := os.Stat(reference)
		if err != nil {
			return "", fmt.Errorf("baseline fixture mapper %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("baseline fixture mapper %s must be a regular file", name)
		}
		current := filepath.Join(e.root, "internal", "db", filename)
		source, err := baselineMapperSource(current, reference, mapper.function)
		if err != nil {
			return "", fmt.Errorf("baseline fixture mapper %s: %w", name, err)
		}
		merged := filepath.Join(e.directory, "fixture-"+filename+".overlay")
		if err := os.WriteFile(merged, source, 0o600); err != nil {
			return "", err
		}
		overlay.Replace[current] = merged
	}
	path := filepath.Join(e.directory, "baseline-fixture-overlay.json")
	return path, writeJSON(path, overlay)
}

func baselineMapperSource(current, reference, onlyFunction string) ([]byte, error) {
	files := token.NewFileSet()
	candidate, err := parser.ParseFile(files, current, nil, 0)
	if err != nil {
		return nil, err
	}
	baseline, err := parser.ParseFile(files, reference, nil, 0)
	if err != nil {
		return nil, err
	}
	functions := make(map[string]*ast.FuncDecl)
	for _, declaration := range baseline.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && (strings.HasPrefix(function.Name.Name, "build") || strings.HasPrefix(function.Name.Name, "scan")) {
			if onlyFunction == "" || function.Name.Name == onlyFunction {
				functions[function.Name.Name] = function
			}
		}
	}
	if onlyFunction != "" && functions[onlyFunction] == nil {
		return nil, fmt.Errorf("reference mapper is missing %s", onlyFunction)
	}
	replaced := false
	for index, declaration := range candidate.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			if replacement := functions[function.Name.Name]; replacement != nil {
				candidate.Decls[index] = replacement
				replaced = true
			}
		}
	}
	if onlyFunction != "" && !replaced {
		return nil, fmt.Errorf("current mapper is missing %s", onlyFunction)
	}
	var source bytes.Buffer
	if err := format.Node(&source, files, candidate); err != nil {
		return nil, err
	}
	return source.Bytes(), nil
}
