package devinit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"uuid"
)

const registryFileName = "oma-devinit-registry.json"

// registryEntry records one bound environment. Worktree may point at a
// deleted directory; entries with a missing path are orphans that teardown
// can reclaim. Database, Bucket and Namespace let orphan teardown clean
// resources without the original worktree.
type registryEntry struct {
	EnvID     string `json:"env_id"`
	Worktree  string `json:"worktree"`
	InitFile  string `json:"init_file"`
	RedisPort int    `json:"redis_port"`
	Database  string `json:"database"`
	Bucket    string `json:"bucket"`
	Namespace string `json:"namespace"`
	UpdatedAt string `json:"updated_at"`
}

type envRegistry struct {
	Envs map[string]registryEntry `json:"envs"`
}

// Registry resolves the shared environment registry inside the Git common
// directory so every worktree of one clone sees the same bindings.
type Registry struct {
	Path string
}

func OpenRegistry(repoRoot string) (Registry, error) {
	command := exec.Command("git", "rev-parse", "--git-common-dir")
	command.Dir = repoRoot
	output, err := command.Output()
	if err != nil {
		return Registry{}, fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(output))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	return Registry{Path: filepath.Join(commonDir, registryFileName)}, nil
}

// Ensure validates that the registry entry for an existing binding still
// belongs to the same environment, then refreshes the recorded worktree path
// and init file. A matching env_id proves identity, so renamed directories
// keep their environment.
func (r Registry) Ensure(binding EnvBinding, initFile string) (registryEntry, error) {
	var entry registryEntry
	err := r.withLock(func(registry *envRegistry) error {
		current, taken := registry.Envs[binding.Name]
		if !taken {
			return fmt.Errorf("environment %s is not registered; the registry was reset, rebind with an explicit cleanup first", binding.Name)
		}
		if current.EnvID != binding.EnvID {
			return fmt.Errorf("name %s now belongs to a different environment; refusing to take it over", binding.Name)
		}
		if current.Worktree != binding.Worktree && pathExists(current.Worktree) {
			return fmt.Errorf("environment %s is still bound to %s", binding.Name, current.Worktree)
		}
		entry = current
		entry.Worktree = binding.Worktree
		entry.InitFile = initFile
		entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		registry.Envs[binding.Name] = entry
		return nil
	})
	return entry, err
}

// Create registers a new environment with a unique name. Infrastructure is
// the per-environment compose stack, so no resource derivation or numeric
// allocation remains; orphaned names stay reserved until teardown reclaims
// the stack.
func (r Registry) Create(worktree, initFile, baseName string) (EnvBinding, registryEntry, error) {
	var binding EnvBinding
	var entry registryEntry
	err := r.withLock(func(registry *envRegistry) error {
		for name, current := range registry.Envs {
			if current.Worktree == worktree {
				entry = current
				binding = EnvBinding{Name: name, EnvID: current.EnvID, Worktree: worktree, InitFile: initFile, BoundAt: current.UpdatedAt}
				return nil
			}
		}
		name := uniqueName(baseName, registry.Envs)
		entry = registryEntry{
			EnvID:     uuid.NewV4().String(),
			Worktree:  worktree,
			InitFile:  initFile,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		registry.Envs[name] = entry
		binding = EnvBinding{
			Name: name, EnvID: entry.EnvID, Worktree: worktree, InitFile: initFile,
			BoundAt: entry.UpdatedAt,
		}
		return nil
	})
	return binding, entry, err
}

// Orphans lists environments whose recorded worktree path no longer exists.
func (r Registry) Orphans() (map[string]registryEntry, error) {
	registry, err := r.read()
	if err != nil {
		return nil, err
	}
	orphans := map[string]registryEntry{}
	for name, entry := range registry.Envs {
		if !pathExists(entry.Worktree) {
			orphans[name] = entry
		}
	}
	return orphans, nil
}

// Remove deletes a registry entry after its resources were torn down.
func (r Registry) Remove(name string) error {
	return r.withLock(func(registry *envRegistry) error {
		delete(registry.Envs, name)
		return nil
	})
}

// Get returns a registry entry by name.
func (r Registry) Get(name string) (registryEntry, bool, error) {
	registry, err := r.read()
	if err != nil {
		return registryEntry{}, false, err
	}
	entry, found := registry.Envs[name]
	return entry, found, nil
}

func uniqueName(base string, live map[string]registryEntry) string {
	if _, taken := live[base]; !taken {
		return base
	}
	for suffix := 0; ; suffix++ {
		candidate := fmt.Sprintf("%s-%04d", base, suffix)
		if _, taken := live[candidate]; !taken {
			return candidate
		}
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (r Registry) read() (envRegistry, error) {
	registry := envRegistry{Envs: map[string]registryEntry{}}
	data, err := os.ReadFile(r.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return registry, nil
		}
		return envRegistry{}, err
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return envRegistry{}, fmt.Errorf("parse environment registry %s: %w", r.Path, err)
	}
	if registry.Envs == nil {
		registry.Envs = map[string]registryEntry{}
	}
	return registry, nil
}

func (r Registry) withLock(update func(*envRegistry) error) error {
	lockPath := r.Path + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	registry, err := r.read()
	if err != nil {
		return err
	}
	if err := update(&registry); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.Path)
}
