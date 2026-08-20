package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestConfigExampleContainsSupportedFields(t *testing.T) {
	examplePath := repositoryFilePath(t, "config", "config.example.yaml")
	got := configDocumentPaths(t, examplePath)
	want := []string{
		"anthropic_upstream",
		"anthropic_upstream.api_key",
		"web_search",
		"web_search.max_server_tool_iterations",
		"web_search.provider",
		"web_search.timeout",
		"web_search.providers",
		"web_search.providers.brave",
		"web_search.providers.brave.api_key",
		"web_search.providers.brave.endpoint",
		"web_search.providers.brave.options",
		"web_search.providers.brave.options.country",
		"web_search.providers.brave.options.search_language",
		"web_search.providers.brave.options.ui_language",
		"web_search.providers.brave.options.freshness",
		"web_search.providers.brave.options.start_published_at",
		"web_search.providers.brave.options.end_published_at",
		"web_search.providers.brave.options.safe_search",
		"web_search.providers.brave.options.spellcheck",
		"web_search.providers.brave.options.result_filter",
		"web_search.providers.brave.options.goggles",
		"web_search.providers.brave.options.extra_snippets",
		"web_search.providers.brave.options.units",
		"web_search.providers.tavily",
		"web_search.providers.tavily.api_key",
		"web_search.providers.tavily.endpoint",
		"database",
		"database.url",
		"e2b",
		"e2b.api_key",
		"e2b.api_url",
		"env",
		"redis",
		"redis.url",
		"server",
		"server.addr",
		"storage",
		"storage.s3",
		"storage.s3.access_key_id",
		"storage.s3.bucket",
		"storage.s3.endpoint",
		"storage.s3.force_path_style",
		"storage.s3.region",
		"storage.s3.secret_access_key",
		"storage.type",
		"vault",
		"vault.master_key",
		"vault.master_key.kek",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("config.example.yaml fields =\n%s\nwant supported-field contract =\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestConfigurationReferenceCoversConfigYAMLContract(t *testing.T) {
	referencePath := repositoryFilePath(t, "docs", "configuration-reference.yaml")
	validateConfigTestFile(t, referencePath)

	got := configDocumentPaths(t, referencePath)
	wantSet := make(map[string]struct{})
	collectConfigTypePaths(reflect.TypeFor[Config](), "", wantSet)
	want := sortedConfigPaths(wantSet)
	if !configPathsMatch(got, want) {
		t.Fatalf("configuration reference fields =\n%s\nwant Config YAML contract =\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestYAMLInputCoversRuntimeConfigContract(t *testing.T) {
	runtimePaths := make(map[string]struct{})
	collectConfigTypePaths(reflect.TypeFor[Config](), "", runtimePaths)
	inputPaths := make(map[string]struct{})
	collectConfigTypePaths(reflect.TypeFor[yamlConfig](), "", inputPaths)

	got := sortedConfigPaths(inputPaths)
	want := sortedConfigPaths(runtimePaths)
	if !configPathsMatch(got, want) {
		t.Fatalf("YAML input fields =\n%s\nwant runtime Config contract =\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func repositoryFilePath(t *testing.T, elements ...string) string {
	t.Helper()
	parts := append([]string{"..", ".."}, elements...)
	path, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("resolve repository file path: %v", err)
	}
	return path
}

func configDocumentPaths(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode %q: %v", path, err)
	}
	paths := make(map[string]struct{})
	collectConfigDocumentPaths(&document, "", paths)
	return sortedConfigPaths(paths)
}

func collectConfigDocumentPaths(node *yaml.Node, prefix string, paths map[string]struct{}) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			collectConfigDocumentPaths(child, prefix, paths)
		}
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			path := joinConfigPath(prefix, node.Content[index].Value)
			paths[path] = struct{}{}
			collectConfigDocumentPaths(node.Content[index+1], path, paths)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			collectConfigDocumentPaths(child, prefix+"[]", paths)
		}
	}
}

func collectConfigTypePaths(configType reflect.Type, prefix string, paths map[string]struct{}) {
	for index := range configType.NumField() {
		field := configType.Field(index)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		path := joinConfigPath(prefix, name)
		paths[path] = struct{}{}

		fieldType := yamlValueType(field.Type)
		switch fieldType.Kind() {
		case reflect.Struct:
			collectConfigTypePaths(fieldType, path, paths)
		case reflect.Slice:
			if fieldType.Elem().Kind() == reflect.Struct {
				collectConfigTypePaths(fieldType.Elem(), path+"[]", paths)
			}
		case reflect.Map:
			mapPath := path + ".*"
			paths[mapPath] = struct{}{}
			if fieldType.Elem().Kind() == reflect.Struct {
				collectConfigTypePaths(fieldType.Elem(), mapPath, paths)
			}
		}
	}
}

func configPathsMatch(actual, expected []string) bool {
	for _, path := range actual {
		found := false
		for _, pattern := range expected {
			if configPathMatches(pattern, path) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, pattern := range expected {
		found := false
		for _, path := range actual {
			if configPathMatches(pattern, path) {
				found = true
				break
			}
		}
		if !found && !strings.HasSuffix(pattern, ".*") {
			return false
		}
	}
	return true
}

func configPathMatches(pattern, actual string) bool {
	patternParts := strings.Split(pattern, ".")
	actualParts := strings.Split(actual, ".")
	if len(patternParts) != len(actualParts) {
		return false
	}
	for index, patternPart := range patternParts {
		if patternPart != "*" && patternPart != actualParts[index] {
			return false
		}
	}
	return true
}

func joinConfigPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func sortedConfigPaths(paths map[string]struct{}) []string {
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	slices.Sort(result)
	return result
}
