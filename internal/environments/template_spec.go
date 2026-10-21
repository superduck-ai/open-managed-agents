package environments

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const (
	maxInitScriptBytes          = 64 << 10
	maxEnvironmentVariables     = 64
	maxEnvironmentVariableBytes = 64 << 10
)

var environmentVariableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type templateResources struct {
	CPU    uint32 `json:"cpu,omitempty"`
	Memory uint32 `json:"memory,omitempty"`
	Disk   uint32 `json:"disk,omitempty"`
}

func (r *templateResources) UnmarshalJSON(data []byte) error {
	type resourceFields templateResources
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*resourceFields)(r))
}

func (r templateResources) empty() bool {
	return r == templateResources{}
}

type templateConfigFields struct {
	InitScript  string             `json:"init_script"`
	Environment map[string]string  `json:"environment"`
	Resources   *templateResources `json:"resources"`
}

type environmentBuildSpec struct {
	Packages   *environmentPackages
	InitScript string
	Resources  templateResources
}

func (s *environmentBuildSpec) requiresTemplate() bool {
	return s != nil && (s.InitScript != "" || !s.Resources.empty())
}

func (s *environmentBuildSpec) needsImage() bool {
	return s != nil && (s.Packages != nil || s.InitScript != "")
}

func decodeEnvironmentBuildSpec(raw json.RawMessage) (*environmentBuildSpec, error) {
	packages, err := decodeEnvironmentPackages(raw)
	if err != nil {
		return nil, err
	}
	fields, err := decodeTemplateConfigFields(raw)
	if err != nil {
		return nil, err
	}
	spec := &environmentBuildSpec{Packages: packages, InitScript: fields.InitScript}
	if fields.Resources != nil {
		spec.Resources = *fields.Resources
	}
	if spec.Packages == nil && !spec.requiresTemplate() {
		return nil, nil
	}
	return spec, nil
}

func decodeTemplateConfigFields(raw json.RawMessage) (templateConfigFields, error) {
	var cloud struct {
		Type string `json:"type"`
		templateConfigFields
	}
	if err := json.Unmarshal(raw, &cloud); err != nil {
		return templateConfigFields{}, fmt.Errorf("decode environment config: %w", err)
	}
	if cloud.Type != "cloud" {
		return templateConfigFields{}, nil
	}
	fields := cloud.templateConfigFields
	if err := validateInitScript(fields.InitScript); err != nil {
		return templateConfigFields{}, err
	}
	if err := validateEnvironmentVariables(fields.Environment); err != nil {
		return templateConfigFields{}, err
	}
	return fields, nil
}

func environmentVariablesFromConfig(raw json.RawMessage) (map[string]string, error) {
	fields, err := decodeTemplateConfigFields(raw)
	if err != nil {
		return nil, err
	}
	return fields.Environment, nil
}

func sameBuildSpec(previous, next *environmentBuildSpec) bool {
	if previous == nil || next == nil {
		return previous == next
	}
	return samePackages(previous.Packages, next.Packages) &&
		previous.InitScript == next.InitScript &&
		previous.Resources == next.Resources
}

func normalizeTemplateConfigFields(fields map[string]json.RawMessage, base map[string]any) error {
	if raw, ok := fields["init_script"]; ok {
		script, err := normalizeInitScript(raw)
		if err != nil {
			return err
		}
		setOrDelete(base, "init_script", script, script != "")
	}
	if raw, ok := fields["environment"]; ok {
		variables, err := normalizeEnvironmentVariables(raw)
		if err != nil {
			return err
		}
		setOrDelete(base, "environment", variables, len(variables) > 0)
	}
	if raw, ok := fields["resources"]; ok {
		resources, err := normalizeTemplateResources(raw)
		if err != nil {
			return err
		}
		setOrDelete(base, "resources", resources, !resources.empty())
	}
	return nil
}

func setOrDelete(base map[string]any, key string, value any, keep bool) {
	if keep {
		base[key] = value
		return
	}
	delete(base, key)
}

func normalizeInitScript(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", nil
	}
	var script string
	if err := json.Unmarshal(raw, &script); err != nil {
		return "", errors.New("config.init_script must be a string or null")
	}
	script = strings.TrimSpace(script)
	return script, validateInitScript(script)
}

func validateInitScript(script string) error {
	switch {
	case len(script) > maxInitScriptBytes:
		return errors.New("config.init_script exceeds the 64 KiB limit")
	case strings.ContainsRune(script, 0):
		return errors.New("config.init_script must not contain NUL characters")
	case packageCredentialURLPattern.MatchString(script):
		return errors.New("config.init_script must not contain URL credentials")
	}
	return nil
}

func normalizeEnvironmentVariables(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var variables map[string]string
	if err := json.Unmarshal(raw, &variables); err != nil {
		return nil, errors.New("config.environment must be an object of string values or null")
	}
	return variables, validateEnvironmentVariables(variables)
}

func validateEnvironmentVariables(variables map[string]string) error {
	if len(variables) > maxEnvironmentVariables {
		return fmt.Errorf("config.environment must not contain more than %d variables", maxEnvironmentVariables)
	}
	total := 0
	for name, value := range variables {
		if !environmentVariableNamePattern.MatchString(name) {
			return errors.New("config.environment keys must be valid environment variable names")
		}
		if strings.ContainsRune(value, 0) {
			return errors.New("config.environment values must not contain NUL characters")
		}
		total += len(name) + len(value)
	}
	if total > maxEnvironmentVariableBytes {
		return errors.New("config.environment exceeds the 64 KiB limit")
	}
	return nil
}

func normalizeTemplateResources(raw json.RawMessage) (templateResources, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return templateResources{}, nil
	}
	var resources templateResources
	if err := json.Unmarshal(raw, &resources); err != nil {
		return templateResources{}, errors.New("config.resources must be an object with optional positive integer cpu, memory and disk")
	}
	return resources, nil
}

func validateTemplateSpecPolicy(raw json.RawMessage, prebuild config.EnvironmentPrebuildConfig) error {
	fields, err := decodeTemplateConfigFields(raw)
	if err != nil {
		return err
	}
	if fields.InitScript == "" && (fields.Resources == nil || fields.Resources.empty()) {
		return nil
	}
	if !prebuild.Enabled {
		return errors.New("config.init_script and config.resources require environment prebuilds to be enabled")
	}
	if fields.Resources == nil {
		return nil
	}
	limits := prebuild.Template.MaxResources
	for _, dimension := range []struct {
		name         string
		value, limit uint32
	}{
		{"cpu", fields.Resources.CPU, limits.CPU},
		{"memory", fields.Resources.Memory, limits.Memory},
		{"disk", fields.Resources.Disk, limits.Disk},
	} {
		if dimension.value == 0 {
			continue
		}
		if dimension.limit == 0 {
			return fmt.Errorf("config.resources.%s cannot be customized on this deployment", dimension.name)
		}
		if dimension.value > dimension.limit {
			return fmt.Errorf("config.resources.%s must not exceed %d", dimension.name, dimension.limit)
		}
	}
	return nil
}

func sandboxEnvironmentVariables(configured, platform map[string]string) map[string]string {
	merged := maps.Clone(configured)
	if merged == nil {
		merged = map[string]string{}
	}
	maps.Copy(merged, platform)
	return merged
}

func platformResourcesForResponse(raw json.RawMessage) *templateResources {
	resources, err := normalizeTemplateResources(raw)
	if err != nil || resources.empty() {
		return nil
	}
	return &resources
}
