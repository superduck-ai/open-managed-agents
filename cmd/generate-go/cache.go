package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type generationCache struct {
	Inputs  string `json:"inputs"`
	Outputs string `json:"outputs"`
}

type listedModule struct {
	Dir     string        `json:"Dir"`
	Main    bool          `json:"Main"`
	Version string        `json:"Version"`
	Replace *listedModule `json:"Replace"`
}

type listedPackage struct {
	Dir        string            `json:"Dir"`
	Module     *listedModule     `json:"Module"`
	Error      *json.RawMessage  `json:"Error"`
	DepsErrors []json.RawMessage `json:"DepsErrors"`
}

func inputDigest(ctx context.Context, root string) (string, error) {
	environment, err := goCommand(ctx, root, "env", "-json", "GOVERSION", "GOROOT", "GOTOOLCHAIN", "GOOS", "GOARCH", "GOAMD64", "GOARM", "GOARM64", "GO386", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM", "GOEXPERIMENT", "GOFLAGS", "CGO_ENABLED", "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS", "GOWORK", "GOMOD", "GOPATH", "GOMODCACHE").Output()
	if err != nil {
		return "", err
	}
	var values map[string]string
	if err := json.Unmarshal(environment, &values); err != nil {
		return "", err
	}
	for _, flag := range []string{"-overlay", "-modfile"} {
		if strings.Contains(values["GOFLAGS"], flag) {
			return "", fmt.Errorf("cache unavailable with %s", flag)
		}
	}
	paths := map[string]bool{}
	for _, directory := range []string{"internal/db", "cmd/generate-go"} {
		if err := collectSources(filepath.Join(root, directory), paths); err != nil {
			return "", err
		}
	}
	if err := collectDependencies(ctx, root, paths); err != nil {
		return "", err
	}
	for _, path := range []string{filepath.Join(root, "scripts/generate-go.sh"), filepath.Join(root, "go.mod"), filepath.Join(root, "go.sum")} {
		paths[path] = true
	}
	if workspace := values["GOWORK"]; workspace != "" && workspace != "off" {
		paths[workspace] = true
		if err := addExisting(workspace+".sum", paths); err != nil {
			return "", err
		}
	}
	return digestFiles(root, paths, environment)
}

func collectDependencies(ctx context.Context, root string, paths map[string]bool) error {
	output, err := goCommand(ctx, root, "list", "-deps", "-e", "-json", "./internal/db", "github.com/superduck-ai/yourbatis/cmd/sqlmapgen").Output()
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if pkg.Error != nil || len(pkg.DepsErrors) > 0 {
			return fmt.Errorf("cannot resolve generation dependencies in %s", pkg.Dir)
		}
		if pkg.Module == nil {
			continue
		}
		module := pkg.Module
		if module.Replace != nil {
			module = module.Replace
		}
		localPath, err := filepath.Rel(root, pkg.Dir)
		if err != nil {
			return err
		}
		inRepository := localPath != ".." && !strings.HasPrefix(localPath, ".."+string(filepath.Separator))
		if !module.Main && module.Version != "" && !inRepository {
			continue
		}
		if err := collectSources(pkg.Dir, paths); err != nil {
			return err
		}
		for _, name := range []string{"go.mod", "go.sum"} {
			if err := addExisting(filepath.Join(module.Dir, name), paths); err != nil {
				return err
			}
		}
		if err := addExisting(filepath.Join(root, "vendor/modules.txt"), paths); err != nil {
			return err
		}
	}
}

func addExisting(path string, paths map[string]bool) error {
	_, err := os.Stat(path)
	if err == nil {
		paths[path] = true
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func collectSources(directory string, paths map[string]bool) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".sqlmap.gen.go") {
			continue
		}
		if strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".xml") {
			paths[filepath.Join(directory, name)] = true
		}
	}
	return nil
}

func outputPaths(root string) (map[string]bool, error) {
	paths := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "internal/db"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sqlmap.gen.go") {
			paths[path] = true
		}
		return nil
	})
	return paths, err
}

func outputDigest(root string) (string, error) {
	paths, err := outputPaths(root)
	if err != nil {
		return "", err
	}
	return digestFiles(root, paths, nil)
}

func digestFiles(root string, paths map[string]bool, extra []byte) (string, error) {
	names := make([]string, 0, len(paths))
	for path := range paths {
		names = append(names, path)
	}
	slices.Sort(names)
	hash := sha256.New()
	hash.Write(extra)
	for _, path := range names {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(hash, "\x00%s\x00%x", name, sum)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func cacheMatches(root, path, inputs string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var cache generationCache
	if json.Unmarshal(data, &cache) != nil || cache.Inputs != inputs {
		return false
	}
	outputs, err := outputDigest(root)
	return err == nil && outputs == cache.Outputs
}

func removeCache(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func removeOutputs(root string) error {
	paths, err := outputPaths(root)
	if err != nil {
		return err
	}
	for path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func saveCache(directory, path string, cache generationCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, "cache-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
