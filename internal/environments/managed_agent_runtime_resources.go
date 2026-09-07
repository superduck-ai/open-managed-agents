package environments

import (
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type managedAgentRuntimeResources struct {
	sources      []json.RawMessage
	workDir      string
	memoryMounts []memoryRuntimeMount
	// invalidMemoryResources holds the session_resource IDs whose memory
	// snapshot could not be parsed. Launch is fail-closed on these: a store the
	// caller attached must never be silently left unmounted.
	invalidMemoryResources []string
}

type githubRepositoryRuntimePayload struct {
	URL       string          `json:"url"`
	MountPath string          `json:"mount_path"`
	Checkout  json.RawMessage `json:"checkout"`
}

type gitRepositoryRuntimeSource struct {
	Type      string          `json:"type"`
	URL       string          `json:"url"`
	MountPath string          `json:"mount_path"`
	Checkout  json.RawMessage `json:"checkout,omitempty"`
}

func resolveManagedAgentRuntimeResources(resources []db.SessionResource) managedAgentRuntimeResources {
	resolved := managedAgentRuntimeResources{
		sources: make([]json.RawMessage, 0, len(resources)),
		workDir: defaultEnvironmentWorkDir,
	}
	var workDirResource *db.SessionResource
	for index := range resources {
		resource := &resources[index]
		switch resource.ResourceType {
		case "github_repository":
			payload, ok := parseGitHubRepositoryRuntimePayload(resource.Payload)
			if !ok {
				continue
			}
			if payload.MountPath != "" &&
				(workDirResource == nil || repositoryAttachedBefore(*resource, *workDirResource)) {
				workDirResource = resource
				resolved.workDir = payload.MountPath
			}
			source, ok := gitRepositoryRuntimeSourceJSON(payload)
			if ok {
				resolved.sources = append(resolved.sources, source)
			}
		case sessionresource.MemoryStoreType:
			mount, ok := parseMemoryRuntimeMount(resource.Payload)
			if !ok {
				resolved.invalidMemoryResources = append(resolved.invalidMemoryResources, resource.ExternalID)
				continue
			}
			resolved.memoryMounts = append(resolved.memoryMounts, mount)
		}
	}
	return resolved
}

func parseGitHubRepositoryRuntimePayload(raw json.RawMessage) (githubRepositoryRuntimePayload, bool) {
	var payload githubRepositoryRuntimePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return githubRepositoryRuntimePayload{}, false
	}
	payload.URL = strings.TrimSpace(payload.URL)
	payload.MountPath = strings.TrimSpace(payload.MountPath)
	return payload, true
}

func gitRepositoryRuntimeSourceJSON(payload githubRepositoryRuntimePayload) (json.RawMessage, bool) {
	if payload.URL == "" || payload.MountPath == "" {
		return nil, false
	}
	if len(payload.Checkout) > 0 && !json.Valid(payload.Checkout) {
		payload.Checkout = nil
	}
	raw, err := json.Marshal(gitRepositoryRuntimeSource{
		Type:      "git_repository",
		URL:       payload.URL,
		MountPath: payload.MountPath,
		Checkout:  payload.Checkout,
	})
	return raw, err == nil
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
