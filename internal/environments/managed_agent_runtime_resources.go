package environments

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type managedAgentRuntimeResources struct {
	sources []json.RawMessage
	workDir string
}

type gitRepositoryRuntimeSource struct {
	Type      string                   `json:"type"`
	GitInfo   gitRepositoryRuntimeInfo `json:"git_info"`
	MountPath string                   `json:"mount_path"`
}

type gitRepositoryRuntimeInfo struct {
	Type string `json:"type"`
	Repo string `json:"repo"`
	URL  string `json:"url"`
	Ref  string `json:"ref,omitempty"`
}

// resolveManagedAgentRuntimeResources excludes tokens; the outbound proxy injects credentials.
func resolveManagedAgentRuntimeResources(resources []db.SessionResource) (managedAgentRuntimeResources, error) {
	resolved := managedAgentRuntimeResources{
		sources: make([]json.RawMessage, 0, len(resources)),
		workDir: defaultEnvironmentWorkDir,
	}
	var workDirResource *db.SessionResource
	var gitSpecs []sessionresource.GitRepositorySpec
	for index := range resources {
		resource := &resources[index]
		switch resource.ResourceType {
		case sessionresource.GitRepositoryType:
			spec, err := sessionresource.ParseStoredGitRepositorySpec(resource.Payload)
			if err != nil {
				return managedAgentRuntimeResources{}, fmt.Errorf("git resource %s: %w", resource.ExternalID, err)
			}
			gitSpecs = append(gitSpecs, spec)
			// Use the earliest attached repository, independent of query order.
			if workDirResource == nil || repositoryAttachedBefore(*resource, *workDirResource) {
				workDirResource = resource
				resolved.workDir = spec.MountPath
			}
			// The EM wire contract uses the existing string ref. Preserve the
			// public branch intent with a fully qualified ref to avoid tag ambiguity.
			var ref string
			if spec.Checkout != nil {
				ref = spec.Checkout.SHA
				if spec.Checkout.Type == "branch" {
					ref = "refs/heads/" + spec.Checkout.Name
				}
			}
			repositoryURL, _ := url.Parse(spec.URL)
			source, err := json.Marshal(gitRepositoryRuntimeSource{
				Type: "git_repository",
				GitInfo: gitRepositoryRuntimeInfo{
					// URL-based resources use a generic Git type; signing requires it,
					// while EM uses the explicit URL to access the repository.
					Type: "git",
					Repo: strings.TrimPrefix(repositoryURL.Path, "/"), URL: spec.URL, Ref: ref,
				},
				MountPath: spec.MountPath,
			})
			if err != nil {
				return managedAgentRuntimeResources{}, err
			}
			resolved.sources = append(resolved.sources, source)
		case "memory_store":
			if source, ok := opaqueRuntimeSourceJSON(resource.Payload); ok {
				resolved.sources = append(resolved.sources, source)
			}
		}
	}
	if err := sessionresource.ValidateGitRepositoryConflicts(gitSpecs); err != nil {
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
