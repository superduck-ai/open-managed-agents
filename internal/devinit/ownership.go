package devinit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const environmentIDLabel = "oma.devinit.env_id"

func lockStack(binding EnvBinding) (func(), error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return acquireInitLock(filepath.Join(cache, "oma", "dev-init", composeProjectName(binding)))
}

func checkStackOwnership(ctx context.Context, binding EnvBinding) error {
	for _, kind := range []string{"container", "volume", "network"} {
		if err := checkDockerResources(ctx, binding, kind); err != nil {
			return err
		}
	}
	return nil
}

func checkDockerResources(ctx context.Context, binding EnvBinding, kind string) error {
	resources, err := listDockerResources(ctx, binding, kind)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if err := checkDockerResource(ctx, binding, kind, resource); err != nil {
			return err
		}
	}
	return nil
}

func listDockerResources(ctx context.Context, binding EnvBinding, kind string) ([]string, error) {
	args := []string{kind, "ls", "-q", "--filter", "label=com.docker.compose.project=" + composeProjectName(binding)}
	if kind == "container" {
		args = append(args, "--all")
	}
	output, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("list %s resources for %s: %w", kind, binding.Name, err)
	}
	return strings.Fields(string(output)), nil
}

func checkStackRemoved(ctx context.Context, binding EnvBinding) error {
	var remaining []string
	for _, kind := range []string{"container", "volume", "network"} {
		resources, err := listDockerResources(ctx, binding, kind)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			remaining = append(remaining, kind+" "+resource)
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("compose project %s still has resources after teardown: %s", composeProjectName(binding), strings.Join(remaining, ", "))
	}
	return nil
}

func checkDockerResource(ctx context.Context, binding EnvBinding, kind, resource string) error {
	format := "{{json .Labels}}"
	if kind == "container" {
		format = "{{json .Config.Labels}}"
	}
	output, err := exec.CommandContext(ctx, "docker", kind, "inspect", "--format", format, resource).Output()
	if err != nil {
		return fmt.Errorf("inspect %s %s: %w", kind, resource, err)
	}
	var labels map[string]string
	if err := json.Unmarshal(output, &labels); err != nil {
		return fmt.Errorf("decode %s ownership: %w", kind, err)
	}
	return validateStackOwner(binding, kind+" "+resource, labels)
}

func validateStackOwner(binding EnvBinding, resource string, labels map[string]string) error {
	if binding.EnvID == "" || labels[environmentIDLabel] != binding.EnvID {
		return fmt.Errorf("%s is not owned by environment %s (%s); refusing to reuse or remove it", resource, binding.Name, binding.EnvID)
	}
	return nil
}
