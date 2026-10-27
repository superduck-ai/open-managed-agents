package devinit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type TeardownOptions struct {
	Yes bool
	// Orphans tears down every registered environment whose worktree path is
	// missing, reusing the resources recorded in the registry.
	Orphans bool
}

func Teardown(ctx context.Context, repoRoot string, opts TeardownOptions) ([]string, error) {
	repoRoot, err := filepathEvalSymlinks(repoRoot)
	if err != nil {
		return nil, err
	}
	registry, err := OpenRegistry(repoRoot)
	if err != nil {
		return nil, err
	}
	targets, err := teardownTargets(repoRoot, registry, opts)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}
	var removed []string
	for _, target := range targets {
		if !opts.Yes {
			return removed, fmt.Errorf("refusing to remove %s without --yes (compose project %s and its volumes)", target.name, composeProjectName(EnvBinding{Name: target.name, EnvID: target.entry.EnvID}))
		}
		if err := teardownOne(ctx, repoRoot, registry, target); err != nil {
			return removed, fmt.Errorf("teardown %s: %w", target.name, err)
		}
		removed = append(removed, target.name)
	}
	return removed, nil
}

type teardownTarget struct {
	name    string
	entry   registryEntry
	binding EnvBinding
	local   bool
}

func teardownTargets(repoRoot string, registry Registry, opts TeardownOptions) ([]teardownTarget, error) {
	if opts.Orphans {
		orphans, err := registry.Orphans()
		if err != nil {
			return nil, err
		}
		targets := make([]teardownTarget, 0, len(orphans))
		for name, entry := range orphans {
			targets = append(targets, teardownTarget{name: name, entry: entry})
		}
		return targets, nil
	}
	binding, found, err := LoadBinding(envFilePath(repoRoot))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("environment is not bound (no .run/env.json)")
	}
	entry, found, err := registry.Get(binding.Name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("environment %s is not registered", binding.Name)
	}
	if entry.Worktree != repoRoot || binding.Worktree != repoRoot {
		return nil, fmt.Errorf("environment %s does not belong to the current worktree; refusing to tear it down", binding.Name)
	}
	if entry.EnvID != binding.EnvID {
		return nil, fmt.Errorf("registered environment %s belongs to a different identity; refusing to tear it down", binding.Name)
	}
	return []teardownTarget{{name: binding.Name, entry: entry, binding: binding, local: true}}, nil
}

func teardownOne(ctx context.Context, repoRoot string, registry Registry, target teardownTarget) error {
	if err := TeardownStack(ctx, repoRoot, EnvBinding{Name: target.name, EnvID: target.entry.EnvID}); err != nil {
		return err
	}
	removePortlessAlias("oma-" + target.name + "-api")
	removePortlessAlias("oma-" + target.name + "-web")
	if err := registry.Remove(target.name); err != nil {
		return err
	}
	if target.local {
		if err := removeLocalState(target.binding); err != nil {
			return err
		}
	}
	return nil
}

func removePortlessAlias(route string) {
	command := exec.Command("portless", "alias", "--remove", route)
	_, _ = command.Output()
}

func removeLocalState(binding EnvBinding) error {
	if err := os.Remove(envFilePath(binding.Worktree)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(generatedDir(binding.Worktree))
}

func teardownCommand(ctx context.Context, repoRoot string, binding EnvBinding) *exec.Cmd {
	command := exec.CommandContext(ctx, "docker", "compose", "-p", composeProjectName(binding),
		"--project-directory", repoRoot, "-f", "deploy/docker-compose/dev-env.yaml",
		"down", "--volumes", "--remove-orphans")
	command.Dir = repoRoot
	command.Env = os.Environ()
	for _, key := range []string{"OMA_PG_PORT", "OMA_REDIS_PORT", "OMA_NATS_PORT", "OMA_NATS_PORT_2", "OMA_NATS_PORT_3", "OMA_MINIO_PORT", "OMA_E2B_PORT"} {
		command.Env = append(command.Env, key+"=1")
	}
	command.Env = append(command.Env, "OMA_WORKTREE="+repoRoot, "OMA_ENV_NAME="+binding.Name,
		"OMA_ENV_ID="+binding.EnvID, "OMA_ENVD_BIN_DIR="+filepath.Join(repoRoot, "envd-bin"),
		"OMA_E2B_CONFIG="+filepath.Join(generatedDir(repoRoot), "e2b-local.yaml"))
	return command
}
