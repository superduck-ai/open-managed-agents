package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	configFileEnv         = "CONFIG_FILE"
	configDirectoryName   = "config"
	configFileName        = "config.yaml"
	defaultConfigFilePath = configDirectoryName + "/" + configFileName
)

func findConfigFile() (string, bool, error) {
	if configuredPath, ok := os.LookupEnv(configFileEnv); ok {
		path, err := absoluteConfigFilePath(configuredPath)
		if err != nil {
			return "", false, err
		}
		if err := validateConfigFile(path); err != nil {
			return "", false, err
		}
		return path, true, nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory: %w", err)
	}
	for {
		path := filepath.Join(dir, configDirectoryName, configFileName)
		if info, statErr := os.Stat(path); statErr == nil {
			if !info.Mode().IsRegular() {
				return "", false, fmt.Errorf("config file %q must be a regular file", path)
			}
			return path, true, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", false, fmt.Errorf("stat config file %q: %w", path, statErr)
		}

		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return "", false, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", false, fmt.Errorf("stat go.mod in %q: %w", dir, statErr)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

func absoluteConfigFilePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("CONFIG_FILE must not be empty")
	}
	expanded, err := expandConfiguredPath(trimmed, "CONFIG_FILE")
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve CONFIG_FILE %q: %w", value, err)
	}
	return absolute, nil
}

func validateConfigFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config file %q must be a regular file", path)
	}
	return nil
}

func loadYAMLConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file %q: %w", path, err)
	}
	input := newYAMLConfig()
	if len(bytes.TrimSpace(data)) == 0 {
		return input.resolve(), nil
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}
	var extraDocument yaml.Node
	if err := decoder.Decode(&extraDocument); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
		}
		return Config{}, fmt.Errorf("decode config file %q: multiple YAML documents are not supported", path)
	}

	if err := validateYAMLNode(&document, reflect.TypeFor[yamlConfig](), nil); err != nil {
		return Config{}, fmt.Errorf("inspect config file %q: %w", path, err)
	}
	if err := document.Decode(&input); err != nil {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}
	return input.resolve(), nil
}

// validateYAMLNode preserves strict unknown-field and null rejection before
// yaml.Node.Decode, which does not expose Decoder.KnownFields.
func validateYAMLNode(node *yaml.Node, target reflect.Type, prefix []string) error {
	return validateYAMLNodeWithAliases(node, target, prefix, make(map[*yaml.Node]struct{}))
}

func validateYAMLNodeWithAliases(node *yaml.Node, target reflect.Type, prefix []string, aliases map[*yaml.Node]struct{}) error {
	if node == nil {
		return nil
	}
	if node.Tag == "!!null" {
		path := strings.Join(prefix, ".")
		if path == "" {
			path = "config"
		}
		return fmt.Errorf("%s must not be null", path)
	}
	target = yamlValueType(target)
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			if err := validateYAMLNodeWithAliases(child, target, prefix, aliases); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		if target.Kind() != reflect.Struct {
			return nil
		}
		fields := yamlFieldTypes(target)
		for index := 0; index+1 < len(node.Content); index += 2 {
			name := node.Content[index].Value
			fieldType, ok := fields[name]
			if !ok {
				return fmt.Errorf("field %s not found in type %s", name, target)
			}
			if err := validateYAMLNodeWithAliases(node.Content[index+1], fieldType, appendYAMLPath(prefix, name), aliases); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
			return nil
		}
		for index, child := range node.Content {
			path := appendYAMLPath(prefix, fmt.Sprintf("[%d]", index))
			if err := validateYAMLNodeWithAliases(child, target.Elem(), path, aliases); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		if _, seen := aliases[node.Alias]; seen {
			return fmt.Errorf("%s contains a recursive YAML alias", strings.Join(prefix, "."))
		}
		aliases[node.Alias] = struct{}{}
		defer delete(aliases, node.Alias)
		return validateYAMLNodeWithAliases(node.Alias, target, prefix, aliases)
	}
	return nil
}

type yamlOptional interface {
	yamlOptional()
}

var yamlOptionalType = reflect.TypeFor[yamlOptional]()

func yamlValueType(target reflect.Type) reflect.Type {
	for {
		if target.Kind() == reflect.Pointer {
			target = target.Elem()
			continue
		}
		if target.Implements(yamlOptionalType) {
			field, ok := target.FieldByName("value")
			if ok {
				target = field.Type
				continue
			}
		}
		return target
	}
}

func yamlFieldTypes(target reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, target.NumField())
	for index := range target.NumField() {
		field := target.Field(index)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		fields[name] = field.Type
	}
	return fields
}

func appendYAMLPath(prefix []string, value string) []string {
	path := make([]string, len(prefix), len(prefix)+1)
	copy(path, prefix)
	return append(path, value)
}

func configFileDirectory(path string) string {
	return filepath.Dir(path)
}

func resolveConfigPaths(cfg *Config, configDir string) error {
	paths := []struct {
		name  string
		value *string
	}{
		{name: "environment_runner.manager_path", value: &cfg.EnvironmentRunner.ManagerPath},
		{name: "environment_runner.claude_path", value: &cfg.EnvironmentRunner.ClaudePath},
		{name: "code_session.otlp_log_root", value: &cfg.CodeSession.OTLPLogRoot},
		{name: "code_session.jwt_signing_private_key_file", value: &cfg.CodeSession.JWTSigningPrivateKeyFile},
		{name: "code_session.upstream_proxy_ca_key_file", value: &cfg.CodeSession.UpstreamProxyCAKeyFile},
	}
	for _, path := range paths {
		if strings.TrimSpace(*path.value) == "" {
			continue
		}
		resolved, err := expandConfiguredPath(*path.value, path.name)
		if err != nil {
			return err
		}
		if configDir != "" && !filepath.IsAbs(resolved) {
			resolved = filepath.Join(configDir, resolved)
		}
		*path.value = filepath.Clean(resolved)
	}
	return nil
}

func expandConfiguredPath(value, name string) (string, error) {
	expanded := strings.TrimSpace(value)
	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %s: resolve home directory: %w", name, err)
		}
		if expanded == "~" {
			expanded = home
		} else {
			expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~/"))
		}
	} else if strings.HasPrefix(expanded, "~") {
		return "", fmt.Errorf("expand %s: user-specific home paths are not supported: %q", name, value)
	}

	missing := ""
	expanded = os.Expand(expanded, func(key string) string {
		resolved, ok := os.LookupEnv(key)
		if !ok && missing == "" {
			missing = key
		}
		return resolved
	})
	if missing != "" {
		return "", fmt.Errorf("expand %s: environment variable %s is not set", name, missing)
	}
	return expanded, nil
}
