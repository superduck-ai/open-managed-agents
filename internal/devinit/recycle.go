package devinit

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

type RecycleOptions struct {
	Yes      bool
	Worktree string
}

type recyclePlan struct {
	root     string
	registry Registry
	binding  EnvBinding
	bound    bool
}

func Recycle(ctx context.Context, repoRoot string, opts RecycleOptions, output io.Writer) error {
	if !opts.Yes {
		return fmt.Errorf("recycle deletes the environment Compose volumes; pass --yes")
	}
	plan, err := prepareRecycle(ctx, repoRoot, opts.Worktree)
	if err != nil {
		return err
	}
	if plan.bound {
		if err := checkStackOwnership(ctx, plan.binding); err != nil {
			return err
		}
	}
	processes, err := findLocalServices(ctx, plan.root)
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "Stopping server, Brain and web:", plan.root)
	if err := stopLocalServices(ctx, plan.root, processes); err != nil {
		return err
	}
	if plan.bound {
		fmt.Fprintln(output, "Removing Compose containers and volumes:", composeProjectName(plan.binding))
		target := teardownTarget{name: plan.binding.Name, binding: plan.binding, local: true}
		target.entry.EnvID = plan.binding.EnvID
		if err := teardownOne(ctx, plan.root, plan.registry, target); err != nil {
			return err
		}
	}
	fmt.Fprintln(output, "Environment cleaned; worktree retained:", plan.root)
	return nil
}

func prepareRecycle(ctx context.Context, repoRoot, target string) (recyclePlan, error) {
	if target == "" {
		target = repoRoot
	}
	root, err := filepathEvalSymlinks(target)
	if err != nil {
		return recyclePlan{}, err
	}
	if err := validateRecycleWorktree(ctx, repoRoot, root); err != nil {
		return recyclePlan{}, err
	}
	registry, err := OpenRegistry(root)
	if err != nil {
		return recyclePlan{}, err
	}
	binding, found, err := LoadBinding(envFilePath(root))
	if err != nil {
		return recyclePlan{}, err
	}
	plan := recyclePlan{root: root, registry: registry, binding: binding, bound: found}
	if !found {
		entries, err := registry.read()
		if err != nil {
			return recyclePlan{}, err
		}
		for _, entry := range entries.Envs {
			if entry.Worktree == root {
				return recyclePlan{}, fmt.Errorf("registered environment has no binding; restore .run/env.json before recycling")
			}
		}
		return plan, nil
	}
	entry, registered, err := registry.Get(binding.Name)
	if err != nil {
		return recyclePlan{}, err
	}
	if !registered || entry.EnvID != binding.EnvID || entry.Worktree != root || binding.Worktree != root {
		return recyclePlan{}, fmt.Errorf("environment binding and registry must both belong to target worktree %s", root)
	}
	return plan, nil
}

func validateRecycleWorktree(ctx context.Context, repoRoot, root string) error {
	data, err := recycleGit(ctx, repoRoot, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	for _, record := range strings.Split(string(data), "\x00\x00") {
		field := strings.Split(record, "\x00")[0]
		if !strings.HasPrefix(field, "worktree ") {
			continue
		}
		path, err := filepathEvalSymlinks(strings.TrimPrefix(field, "worktree "))
		if err == nil && path == root {
			return nil
		}
	}
	return fmt.Errorf("target is not a worktree of this repository: %s", root)
}

func recycleGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = filepath.Clean(root)
	data, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %w: %s", args[0], err, data)
	}
	return data, nil
}
