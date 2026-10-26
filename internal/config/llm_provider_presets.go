package config

import (
	"cmp"
	"crypto/sha256"
	_ "embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

//go:embed model-provider-presets.json
var defaultLLMProviderPresets []byte

var llmPresetIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

type LLMProviderPreset struct {
	Order      int              `json:"order,omitzero"`
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	APIBackend string           `json:"api_backend"`
	BaseURL    string           `json:"base_url"`
	Models     []LLMPresetModel `json:"models"`
}

type LLMPresetModel struct {
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	ContextWindow   *int     `json:"context_window,omitzero"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitzero"`
	InputModalities []string `json:"input_modalities,omitempty"`
}

type llmProviderPresetsFile struct {
	Version   int                 `json:"version"`
	Providers []LLMProviderPreset `json:"providers"`
}

func decodeLLMProviderPresets(data []byte) ([]LLMProviderPreset, error) {
	var file llmProviderPresetsFile
	if err := json.Unmarshal(data, &file, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("decode llm_provider_presets_file: %w", err)
	}
	if file.Version != 1 || file.Providers == nil {
		return nil, errors.New("llm_provider_presets_file requires version 1 and a providers array")
	}
	seen := make(map[string]bool)
	for index := range file.Providers {
		preset := &file.Providers[index]
		if err := normalizeLLMProviderPreset(preset); err != nil {
			return nil, fmt.Errorf("llm_provider_presets_file.providers[%d]: %w", index, err)
		}
		if seen[preset.ID] {
			return nil, fmt.Errorf("llm_provider_presets_file.providers[%d]: duplicate provider id", index)
		}
		seen[preset.ID] = true
	}
	slices.SortStableFunc(file.Providers, func(left, right LLMProviderPreset) int { return cmp.Compare(left.Order, right.Order) })
	return file.Providers, nil
}

func normalizeLLMProviderPreset(preset *LLMProviderPreset) error {
	if preset.Order < 0 || preset.Order > 9007199254740991 {
		return errors.New("order must be a nonnegative safe integer")
	}
	preset.ID = strings.TrimSpace(preset.ID)
	preset.Name = strings.TrimSpace(preset.Name)
	preset.BaseURL = strings.TrimRight(strings.TrimSpace(preset.BaseURL), "/")
	if !llmPresetIDPattern.MatchString(preset.ID) || preset.ID == "organization" {
		return errors.New("id must be a non-reserved provider identifier")
	}
	if preset.Name == "" || utf8.RuneCountInString(preset.Name) > 100 {
		return errors.New("name must contain 1 to 100 characters")
	}
	if preset.APIBackend != "messages" && preset.APIBackend != "chat_completions" {
		return errors.New("api_backend must be messages or chat_completions")
	}
	u, err := url.Parse(preset.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base_url must be an HTTP(S) URL without credentials, query or fragment")
	}
	if len(preset.Models) > 100 {
		return errors.New("models must contain at most 100 entries")
	}
	if preset.Models == nil {
		preset.Models = []LLMPresetModel{}
	}
	seen := make(map[string]bool)
	for index := range preset.Models {
		m := &preset.Models[index]
		m.ID, m.Name = strings.TrimSpace(m.ID), strings.TrimSpace(m.Name)
		if err := validateLLMPresetModel(*m); err != nil {
			return fmt.Errorf("models[%d]: %w", index, err)
		}
		if seen[m.ID] {
			return fmt.Errorf("models[%d]: duplicate model id", index)
		}
		seen[m.ID] = true
	}
	return nil
}

func validateLLMPresetModel(model LLMPresetModel) error {
	if model.ID == "" || utf8.RuneCountInString(model.ID) > 255 || utf8.RuneCountInString(model.Name) > 100 {
		return errors.New("model id must contain 1 to 255 characters and name at most 100 characters")
	}
	for _, value := range []*int{model.ContextWindow, model.MaxOutputTokens} {
		if value != nil && (*value <= 0 || *value > 9007199254740991) {
			return errors.New("model token limits must be positive safe integers")
		}
	}
	if model.InputModalities == nil {
		return nil
	}
	if len(model.InputModalities) == 0 || len(model.InputModalities) > 2 {
		return errors.New("input_modalities must be a nonempty set of text/image")
	}
	for index, modality := range model.InputModalities {
		if (modality != "text" && modality != "image") || slices.Contains(model.InputModalities[:index], modality) {
			return errors.New("input_modalities must be a nonempty set of text/image")
		}
	}
	return nil
}

type LLMProviderPresetCatalog struct {
	Providers []LLMProviderPreset `json:"providers"`
	Revision  string              `json:"revision"`
	Writable  bool                `json:"writable"`
}

var ErrPresetConflict = errors.New("supplier defaults changed; reload before saving")
var ErrPresetReadOnly = errors.New("configure llm_provider_presets_file with a writable JSON file to save defaults")
var ErrPresetNotFound = errors.New("supplier preset not found")

func ReadLLMProviderPresetCatalog(path string) (LLMProviderPresetCatalog, error) {
	data := defaultLLMProviderPresets
	if path != "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return LLMProviderPresetCatalog{}, err
		}
	}
	providers, err := decodeLLMProviderPresets(data)
	if err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	return LLMProviderPresetCatalog{Providers: providers, Revision: fmt.Sprintf("%x", sha256.Sum256(data)), Writable: path != ""}, nil
}

func SaveLLMProviderPresetModels(path, presetID, revision string, models []LLMPresetModel) (LLMProviderPresetCatalog, error) {
	if path == "" {
		return LLMProviderPresetCatalog{}, ErrPresetReadOnly
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	catalog, err := ReadLLMProviderPresetCatalog(path)
	if err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	if revision == "" || revision != catalog.Revision {
		return LLMProviderPresetCatalog{}, ErrPresetConflict
	}
	index := slices.IndexFunc(catalog.Providers, func(preset LLMProviderPreset) bool { return preset.ID == presetID })
	if index < 0 {
		return LLMProviderPresetCatalog{}, ErrPresetNotFound
	}
	catalog.Providers[index].Models = models
	if err := normalizeLLMProviderPreset(&catalog.Providers[index]); err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	data, err := json.Marshal(llmProviderPresetsFile{Version: 1, Providers: catalog.Providers}, jsontext.WithIndent("  "))
	if err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	if err := replaceLLMProviderPresetsFile(path, append(data, '\n')); err != nil {
		return LLMProviderPresetCatalog{}, err
	}
	return ReadLLMProviderPresetCatalog(path)
}

func replaceLLMProviderPresetsFile(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".model-provider-presets-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
