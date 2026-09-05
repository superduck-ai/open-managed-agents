package environments

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type managedAgentRuntimeResources struct {
	sources            []json.RawMessage
	workDir            string
	hasGitRepositories bool
}

type gitRepositoryRuntimeSource struct {
	Type      string                          `json:"type"`
	GitInfo   gitRepositoryRuntimeInfo        `json:"git_info"`
	MountPath string                          `json:"mount_path"`
	Checkout  *sessionresource.GitHubCheckout `json:"checkout,omitempty"`
}

type gitRepositoryRuntimeInfo struct {
	Type string `json:"type"`
	Repo string `json:"repo"`
	Host string `json:"host"`
	URL  string `json:"url"`
}

func resolveManagedAgentRuntimeResources(resources []db.SessionResource) (managedAgentRuntimeResources, error) {
	resolved := managedAgentRuntimeResources{
		sources: make([]json.RawMessage, 0, len(resources)),
		workDir: defaultEnvironmentWorkDir,
	}
	var workDirResource *db.SessionResource
	var gitSpecs []sessionresource.GitHubSpec
	for index := range resources {
		resource := &resources[index]
		switch resource.ResourceType {
		case sessionresource.GitHubRepositoryType:
			spec, err := sessionresource.ParseStoredGitHubSpec(resource.Payload)
			if err != nil {
				return managedAgentRuntimeResources{}, fmt.Errorf("GitHub resource %s: %w", resource.ExternalID, err)
			}
			gitSpecs = append(gitSpecs, spec)
			if workDirResource == nil || repositoryAttachedBefore(*resource, *workDirResource) {
				workDirResource = resource
				resolved.workDir = spec.MountPath
			}
			source, err := json.Marshal(gitRepositoryRuntimeSource{
				Type: "git_repository",
				GitInfo: gitRepositoryRuntimeInfo{
					Type: "github", Repo: strings.TrimPrefix(spec.URL, "https://github.com/"),
					Host: "github", URL: spec.URL,
				},
				MountPath: spec.MountPath, Checkout: spec.Checkout,
			})
			if err != nil {
				return managedAgentRuntimeResources{}, err
			}
			resolved.sources = append(resolved.sources, source)
			resolved.hasGitRepositories = true
		case "memory_store":
			if source, ok := opaqueRuntimeSourceJSON(resource.Payload); ok {
				resolved.sources = append(resolved.sources, source)
			}
		}
	}
	if err := sessionresource.ValidateGitHubSpecs(gitSpecs); err != nil {
		return managedAgentRuntimeResources{}, err
	}
	return resolved, nil
}

func opaqueRuntimeSourceJSON(raw json.RawMessage) (json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, false
	}
	return append(json.RawMessage(nil), raw...), true
}

func repositoryAttachedBefore(candidate, current db.SessionResource) bool {
	if !candidate.CreatedAt.Equal(current.CreatedAt) {
		return candidate.CreatedAt.Before(current.CreatedAt)
	}
	if candidate.UUID != current.UUID {
		return candidate.UUID < current.UUID
	}
	return candidate.ExternalID < current.ExternalID
}
