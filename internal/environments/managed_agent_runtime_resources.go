package environments

import (
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type managedAgentRuntimeResources struct {
	sources []json.RawMessage
	workDir string
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
		case "memory_store":
			if source, ok := opaqueRuntimeSourceJSON(resource.Payload); ok {
				resolved.sources = append(resolved.sources, source)
			}
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
	if candidate.ID != current.ID {
		return candidate.ID < current.ID
	}
	return candidate.ExternalID < current.ExternalID
}
