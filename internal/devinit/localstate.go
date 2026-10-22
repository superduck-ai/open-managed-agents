package devinit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	phasePendingSecrets = "pending-secrets"
	phaseInfra          = "infra"
	phaseMigrated       = "migrated"
	phaseIdentity       = "identity"
	phaseKeys           = "keys"
	phaseProviders      = "providers"
	phaseComplete       = "complete"
)

// EnvBinding is stored in .run/env.json of the bound worktree and travels
// with the directory, so renames keep the environment.
type EnvBinding struct {
	Name     string `json:"name"`
	EnvID    string `json:"env_id"`
	Worktree string `json:"worktree"`
	InitFile string `json:"init_file"`
	BoundAt  string `json:"bound_at"`
}

// State tracks the durable mapping between init-file refs and real resource
// identities, plus the resumable initialization phase.
type State struct {
	IdentityPending bool               `json:"identity_pending,omitempty"`
	EnvID           string             `json:"env_id"`
	Name            string             `json:"name"`
	Phase           string             `json:"phase"`
	Runtime         *RuntimePorts      `json:"runtime,omitempty"`
	Organization    *OrganizationState `json:"organization,omitempty"`
	User            *UserState         `json:"user,omitempty"`
	Workspaces      []WorkspaceState   `json:"workspaces,omitempty"`
	Providers       []ProviderState    `json:"providers,omitempty"`
	CompletedAt     string             `json:"completed_at,omitempty"`
}

// RuntimePorts is kept for state.json compatibility; the stack ports are
// the authoritative record of the per-environment infrastructure.
type RuntimePorts = StackPorts

type OrganizationState struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type UserState struct {
	UUID       string `json:"uuid"`
	ExternalID string `json:"external_id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
}

type WorkspaceState struct {
	Ref         string            `json:"ref"`
	UUID        string            `json:"uuid"`
	ExternalID  string            `json:"external_id"`
	Name        string            `json:"name"`
	IsDefault   bool              `json:"is_default"`
	APIKeys     []APIKeyState     `json:"api_keys,omitempty"`
	Environment *EnvironmentState `json:"environment,omitempty"`
}

type EnvironmentState struct {
	Template   string `json:"template,omitempty"`
	UUID       string `json:"uuid"`
	ExternalID string `json:"external_id"`
	Pending    bool   `json:"pending,omitempty"`
}

type APIKeyState struct {
	Pending    bool   `json:"pending,omitempty"`
	Ref        string `json:"ref"`
	UUID       string `json:"uuid"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
}

type ProviderState struct {
	WorkspaceRef string `json:"workspace_ref"`
	Name         string `json:"name"`
	ExternalID   string `json:"external_id"`
}

// Credentials stores plaintext client keys. Losing it prevents key recovery
// from the database hash and requires an explicit rotation.
type Credentials struct {
	EnvID           string          `json:"env_id"`
	BrainClaimToken string          `json:"brain_claim_token"`
	APIKeys         []APICredential `json:"api_keys"`
}

type APICredential struct {
	WorkspaceRef string `json:"workspace_ref"`
	Ref          string `json:"ref"`
	ExternalID   string `json:"external_id"`
	Name         string `json:"name"`
	Key          string `json:"key"`
}

func envFilePath(repoRoot string) string {
	return filepath.Join(repoRoot, ".run", "env.json")
}

func writePrivateFile(path string, perm os.FileMode, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// SaveBinding persists the environment binding and marks the state directory
// private. The binding replaces the old integer slot identity.
func SaveBinding(layout Layout, binding EnvBinding) error {
	if err := os.MkdirAll(layout.RunDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(layout.StateDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(layout.SecretsDir, 0o700); err != nil {
		return err
	}
	return writePrivateFile(layout.BindingFile, 0o600, binding)
}

func LoadBinding(path string) (EnvBinding, bool, error) {
	var binding EnvBinding
	if err := readJSONFile(path, &binding); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EnvBinding{}, false, nil
		}
		return EnvBinding{}, false, err
	}
	if binding.EnvID == "" || binding.Name == "" {
		return EnvBinding{}, false, fmt.Errorf("binding file %s is incomplete", path)
	}
	return binding, true, nil
}

func SaveState(layout Layout, state State) error {
	return writePrivateFile(layout.StateFile, 0o600, state)
}

func LoadState(stateFile string) (State, bool, error) {
	var state State
	if err := readJSONFile(stateFile, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, false, nil
		}
		return State{}, false, err
	}
	return state, true, nil
}

func SaveCredentials(layout Layout, credentials Credentials) error {
	return writePrivateFile(layout.CredentialsFile, 0o600, credentials)
}

func LoadCredentials(credentialsFile string) (Credentials, bool, error) {
	var credentials Credentials
	if err := readJSONFile(credentialsFile, &credentials); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, false, nil
		}
		return Credentials{}, false, err
	}
	if credentials.EnvID == "" || credentials.BrainClaimToken == "" {
		return Credentials{}, false, fmt.Errorf("credentials file %s is incomplete", credentialsFile)
	}
	return credentials, true, nil
}

// worktreeSlug names the environment after what distinguishes the checkout:
// the parent directory for linked worktrees (e.g. the codex worktree id), or
// the repository basename for the main checkout. The value is pinned in the
// binding at creation time, so later renames keep the environment.
func worktreeSlug(repoRoot string) string {
	gitPath := filepath.Join(repoRoot, ".git")
	if info, err := os.Stat(gitPath); err == nil && !info.IsDir() {
		return slugify(filepath.Base(filepath.Dir(repoRoot)))
	}
	return slugify(filepath.Base(repoRoot))
}

func slugify(base string) string {
	base = strings.ToLower(base)
	var slug strings.Builder
	lastDash := false
	for _, char := range base {
		valid := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		switch {
		case valid:
			slug.WriteRune(char)
			lastDash = false
		case !lastDash:
			slug.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(slug.String(), "-")
	if len(result) > 24 {
		result = strings.TrimRight(result[:24], "-")
	}
	if result == "" {
		return "env"
	}
	return result
}
