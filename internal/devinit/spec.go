package devinit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

const (
	specVersion            = 1
	workspaceCount         = 2
	apiKeysPerWorkspace    = 2
	maxModelProviderCount  = 100
	maxModelIDsPerProvider = 100
)

// Spec is the strict dev-init.yaml contract.
type Spec struct {
	LocalOpenObserve bool
	Version          int
	FixedSecretsDir  string
	OrganizationName string
	UserEmail        string
	UserName         string
	UserRole         string
	Workspaces       []WorkspaceSpec
	ModelProviders   string
}

type WorkspaceSpec struct {
	Ref       string
	Name      string
	IsDefault bool
	APIKeys   []APIKeySpec
}

type APIKeySpec struct {
	Ref  string
	Name string
}

// ModelProvidersSpec is the strict model-providers.yaml contract.
type ModelProvidersSpec struct {
	Version   int
	Providers []ProviderSpec
}

type ProviderSpec struct {
	Slug       string
	Name       string
	APIBackend string
	BaseURL    string
	APIKey     string
	ModelIDs   []string
}

// loadStrictYAML decodes one YAML document into target while rejecting unknown
// fields, null values and multiple documents, matching the runtime config
// boundary behavior.
func loadStrictYAML(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
		return fmt.Errorf("%s must be a YAML mapping", path)
	} else if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(document.Content) == 0 {
		return fmt.Errorf("%s must be a YAML mapping", path)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		return fmt.Errorf("%s: multiple YAML documents are not supported", path)
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a YAML mapping", path)
	}
	if err := rejectUnknownYAMLFields(root, reflect.TypeOf(target).Elem(), path); err != nil {
		return err
	}
	if err := root.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func rejectUnknownYAMLFields(node *yaml.Node, structType reflect.Type, path string) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a YAML mapping", path)
	}
	known := map[string]reflect.Type{}
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" {
			name = field.Name
		}
		known[name] = field.Type
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		fieldType, ok := known[key]
		if !ok {
			return fmt.Errorf("%s: field %q not found in %s", path, key, structType.Name())
		}
		value := node.Content[i+1]
		if value.Tag == "!!null" {
			return fmt.Errorf("%s: %s must not be null", path, key)
		}
		if err := rejectUnknownNestedYAMLFields(value, fieldType, path, key); err != nil {
			return err
		}
	}
	return nil
}

func rejectUnknownNestedYAMLFields(node *yaml.Node, fieldType reflect.Type, path, prefix string) error {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	switch fieldType.Kind() {
	case reflect.Struct:
		if fieldType == reflect.TypeOf(time.Duration(0)) {
			return nil
		}
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: %s must be a mapping", path, prefix)
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			known := false
			var nested reflect.Type
			for j := 0; j < fieldType.NumField(); j++ {
				field := fieldType.Field(j)
				if field.PkgPath != "" {
					continue
				}
				name := strings.Split(field.Tag.Get("yaml"), ",")[0]
				if name == "" {
					name = field.Name
				}
				if name == key {
					known = true
					nested = field.Type
					break
				}
			}
			if !known {
				return fmt.Errorf("%s: field %q not found under %s", path, key, prefix)
			}
			value := node.Content[i+1]
			if value.Tag == "!!null" {
				return fmt.Errorf("%s: %s.%s must not be null", path, prefix, key)
			}
			if err := rejectUnknownNestedYAMLFields(value, nested, path, prefix+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s: %s must be a sequence", path, prefix)
		}
		element := fieldType.Elem()
		for _, item := range node.Content {
			if err := rejectUnknownNestedYAMLFields(item, element, path, prefix+"[]"); err != nil {
				return err
			}
		}
	}
	return nil
}

type rawSpec struct {
	LocalOpenObserve bool   `yaml:"local_openobserve"`
	Version          int    `yaml:"version"`
	FixedSecretsDir  string `yaml:"fixed_secrets_dir"`
	Organization     struct {
		Name string `yaml:"name"`
	} `yaml:"organization"`
	User struct {
		Email string `yaml:"email"`
		Name  string `yaml:"name"`
		Role  string `yaml:"role"`
	} `yaml:"user"`
	Workspaces []struct {
		Ref       string `yaml:"ref"`
		Name      string `yaml:"name"`
		IsDefault bool   `yaml:"is_default"`
		APIKeys   []struct {
			Ref  string `yaml:"ref"`
			Name string `yaml:"name"`
		} `yaml:"api_keys"`
	} `yaml:"workspaces"`
	ModelProvidersFile string `yaml:"model_providers_file"`
}

func LoadSpec(path string) (Spec, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Spec{}, err
	}
	var raw rawSpec
	if err := loadStrictYAML(path, &raw); err != nil {
		return Spec{}, err
	}
	spec := Spec{
		LocalOpenObserve: raw.LocalOpenObserve,
		Version:          raw.Version,
		FixedSecretsDir:  raw.FixedSecretsDir,
		OrganizationName: raw.Organization.Name,
		UserEmail:        raw.User.Email,
		UserName:         raw.User.Name,
		UserRole:         raw.User.Role,
		ModelProviders:   raw.ModelProvidersFile,
	}
	for _, workspace := range raw.Workspaces {
		keys := make([]APIKeySpec, 0, len(workspace.APIKeys))
		for _, key := range workspace.APIKeys {
			keys = append(keys, APIKeySpec{Ref: key.Ref, Name: key.Name})
		}
		spec.Workspaces = append(spec.Workspaces, WorkspaceSpec{
			Ref:       workspace.Ref,
			Name:      workspace.Name,
			IsDefault: workspace.IsDefault,
			APIKeys:   keys,
		})
	}
	if err := spec.validate(); err != nil {
		return Spec{}, fmt.Errorf("%s: %w", path, err)
	}
	if !filepath.IsAbs(spec.ModelProviders) {
		spec.ModelProviders = filepath.Join(filepath.Dir(path), spec.ModelProviders)
	}
	if !filepath.IsAbs(spec.FixedSecretsDir) {
		spec.FixedSecretsDir = filepath.Join(filepath.Dir(path), spec.FixedSecretsDir)
	}
	return spec, nil
}

func (s Spec) validate() error {
	if s.Version != specVersion {
		return fmt.Errorf("version must be %d", specVersion)
	}
	if strings.TrimSpace(s.FixedSecretsDir) == "" {
		return errors.New("fixed_secrets_dir is required")
	}
	if strings.TrimSpace(s.OrganizationName) == "" {
		return errors.New("organization.name is required")
	}
	if err := validateEmail(s.UserEmail); err != nil {
		return fmt.Errorf("user.email: %w", err)
	}
	if strings.TrimSpace(s.UserName) == "" {
		return errors.New("user.name is required")
	}
	if s.UserRole != "admin" {
		return errors.New("user.role must be \"admin\"")
	}
	if len(s.Workspaces) != workspaceCount {
		return fmt.Errorf("workspaces must contain exactly %d entries", workspaceCount)
	}
	refs := map[string]bool{}
	names := map[string]bool{}
	defaults := 0
	for _, workspace := range s.Workspaces {
		if strings.TrimSpace(workspace.Ref) == "" {
			return errors.New("workspace ref is required")
		}
		if refs[workspace.Ref] {
			return fmt.Errorf("workspace ref %q is duplicated", workspace.Ref)
		}
		refs[workspace.Ref] = true
		if strings.TrimSpace(workspace.Name) == "" {
			return fmt.Errorf("workspace %q: name is required", workspace.Ref)
		}
		if names[workspace.Name] {
			return fmt.Errorf("workspace name %q is duplicated", workspace.Name)
		}
		names[workspace.Name] = true
		if workspace.IsDefault {
			defaults++
		}
		if len(workspace.APIKeys) != apiKeysPerWorkspace {
			return fmt.Errorf("workspace %q: api_keys must contain exactly %d entries", workspace.Ref, apiKeysPerWorkspace)
		}
		keyRefs := map[string]bool{}
		for _, key := range workspace.APIKeys {
			if strings.TrimSpace(key.Ref) == "" {
				return fmt.Errorf("workspace %q: api key ref is required", workspace.Ref)
			}
			if keyRefs[key.Ref] {
				return fmt.Errorf("workspace %q: api key ref %q is duplicated", workspace.Ref, key.Ref)
			}
			keyRefs[key.Ref] = true
			if strings.TrimSpace(key.Name) == "" {
				return fmt.Errorf("workspace %q: api key %q: name is required", workspace.Ref, key.Ref)
			}
		}
	}
	if defaults != 1 {
		return fmt.Errorf("workspaces must declare exactly one default workspace")
	}
	if strings.TrimSpace(s.ModelProviders) == "" {
		return errors.New("model_providers_file is required")
	}
	return nil
}

type rawProvider struct {
	Slug       string   `yaml:"slug"`
	Name       string   `yaml:"name"`
	APIBackend string   `yaml:"api_backend"`
	BaseURL    string   `yaml:"base_url"`
	APIKey     string   `yaml:"api_key"`
	APIKeyEnv  string   `yaml:"api_key_env"`
	APIKeyFile string   `yaml:"api_key_file"`
	ModelIDs   []string `yaml:"model_ids"`
}

type rawProvidersFile struct {
	Version   int           `yaml:"version"`
	Providers []rawProvider `yaml:"providers"`
}

// LoadModelProviders resolves provider API keys from the file, environment or
// a sibling secret file. Relative key files resolve against the provider file.
func LoadModelProviders(path string) (ModelProvidersSpec, error) {
	var raw rawProvidersFile
	if err := loadStrictYAML(path, &raw); err != nil {
		return ModelProvidersSpec{}, err
	}
	if raw.Version != specVersion {
		return ModelProvidersSpec{}, fmt.Errorf("%s: version must be %d", path, specVersion)
	}
	if len(raw.Providers) == 0 {
		return ModelProvidersSpec{}, fmt.Errorf("%s: providers must not be empty", path)
	}
	if len(raw.Providers) > maxModelProviderCount {
		return ModelProvidersSpec{}, fmt.Errorf("%s: providers exceed %d", path, maxModelProviderCount)
	}
	spec := ModelProvidersSpec{Version: raw.Version}
	seen := map[string]bool{}
	slugs := map[string]bool{}
	for _, provider := range raw.Providers {
		resolved, err := resolveProvider(path, provider)
		if err != nil {
			return ModelProvidersSpec{}, fmt.Errorf("%s: provider %q: %w", path, provider.Name, err)
		}
		if seen[resolved.Name] {
			return ModelProvidersSpec{}, fmt.Errorf("%s: provider name %q is duplicated", path, resolved.Name)
		}
		if resolved.Slug != "" && slugs[resolved.Slug] {
			return ModelProvidersSpec{}, fmt.Errorf("%s: provider slug %q is duplicated", path, resolved.Slug)
		}
		slugs[resolved.Slug] = true
		seen[resolved.Name] = true
		spec.Providers = append(spec.Providers, resolved)
	}
	return spec, nil
}

func resolveProvider(path string, provider rawProvider) (ProviderSpec, error) {
	slug := strings.TrimSpace(provider.Slug)
	if slug != "" {
		if err := llmproviders.ValidateSlug(slug); err != nil {
			return ProviderSpec{}, err
		}
	}
	if strings.TrimSpace(provider.Name) == "" || len(provider.Name) > 100 {
		return ProviderSpec{}, errors.New("name must be 1-100 characters")
	}
	backend := provider.APIBackend
	if backend == "" {
		backend = llmproviders.Messages
	}
	if !llmproviders.ValidBackend(backend) {
		return ProviderSpec{}, fmt.Errorf("api_backend must be %q or %q", llmproviders.Messages, llmproviders.ChatCompletions)
	}
	baseURL, err := llmproviders.ValidateBaseURL(provider.BaseURL)
	if err != nil {
		return ProviderSpec{}, err
	}
	sources := 0
	apiKey := strings.TrimSpace(provider.APIKey)
	if apiKey != "" {
		sources++
	}
	if provider.APIKeyEnv != "" {
		sources++
	}
	if provider.APIKeyFile != "" {
		sources++
	}
	if sources != 1 {
		return ProviderSpec{}, errors.New("exactly one of api_key, api_key_env or api_key_file is required")
	}
	switch {
	case provider.APIKeyEnv != "":
		value, ok := os.LookupEnv(provider.APIKeyEnv)
		if !ok || strings.TrimSpace(value) == "" {
			return ProviderSpec{}, fmt.Errorf("api_key_env %s is not set", provider.APIKeyEnv)
		}
		apiKey = strings.TrimSpace(value)
	case provider.APIKeyFile != "":
		keyPath := provider.APIKeyFile
		if !filepath.IsAbs(keyPath) {
			keyPath = filepath.Join(filepath.Dir(path), keyPath)
		}
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return ProviderSpec{}, fmt.Errorf("read api_key_file: %w", err)
		}
		apiKey = strings.TrimSpace(string(data))
		if apiKey == "" {
			return ProviderSpec{}, errors.New("api_key_file is empty")
		}
	}
	modelIDs := make([]string, 0, len(provider.ModelIDs))
	seenModel := map[string]bool{}
	for _, modelID := range provider.ModelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" || len(modelID) > 255 {
			return ProviderSpec{}, errors.New("model_ids entries must be 1-255 characters")
		}
		if seenModel[modelID] {
			return ProviderSpec{}, fmt.Errorf("model id %q is duplicated", modelID)
		}
		seenModel[modelID] = true
		modelIDs = append(modelIDs, modelID)
	}
	if len(modelIDs) == 0 || len(modelIDs) > maxModelIDsPerProvider {
		return ProviderSpec{}, fmt.Errorf("model_ids must contain 1-%d entries", maxModelIDsPerProvider)
	}
	return ProviderSpec{
		Slug:       slug,
		Name:       provider.Name,
		APIBackend: backend,
		BaseURL:    baseURL,
		APIKey:     apiKey,
		ModelIDs:   modelIDs,
	}, nil
}

func validateEmail(email string) error {
	if strings.TrimSpace(email) == "" || !strings.Contains(email, "@") {
		return errors.New("must be a valid email address")
	}
	local, domain, found := strings.Cut(email, "@")
	if local == "" || domain == "" || !found || strings.Contains(domain, "..") || !strings.Contains(domain, ".") {
		return errors.New("must be a valid email address")
	}
	return nil
}
