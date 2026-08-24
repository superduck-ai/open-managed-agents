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

type gitRepositoryRuntimeSource struct {
	Type      string          `json:"type"`
	URL       string          `json:"url"`
	MountPath string          `json:"mount_path"`
	Checkout  json.RawMessage `json:"checkout,omitempty"`
}

type memoryStoreRuntimeSource struct {
	Type          string  `json:"type"`
	MemoryStoreID string  `json:"memory_store_id"`
	Access        *string `json:"access,omitempty"`
	Description   *string `json:"description,omitempty"`
	Instructions  *string `json:"instructions,omitempty"`
	MountPath     *string `json:"mount_path,omitempty"`
	Name          *string `json:"name,omitempty"`
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
			repository := resource.GitHubRepository
			if repository == nil {
				continue
			}
			runtimeRepository := *repository
			runtimeRepository.URL = strings.TrimSpace(runtimeRepository.URL)
			runtimeRepository.MountPath = strings.TrimSpace(runtimeRepository.MountPath)
			if runtimeRepository.MountPath != "" &&
				(workDirResource == nil || repositoryAttachedBefore(*resource, *workDirResource)) {
				workDirResource = resource
				resolved.workDir = runtimeRepository.MountPath
			}
			source, ok := gitRepositoryRuntimeSourceJSON(runtimeRepository)
			if ok {
				resolved.sources = append(resolved.sources, source)
			}
		case "memory_store":
			if source, ok := memoryStoreRuntimeSourceJSON(resource.MemoryStore); ok {
				resolved.sources = append(resolved.sources, source)
			}
		}
	}
	return resolved
}

func gitRepositoryRuntimeSourceJSON(payload db.SessionResourceGitHubRepository) (json.RawMessage, bool) {
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

func memoryStoreRuntimeSourceJSON(memoryStore *db.SessionResourceMemoryStore) (json.RawMessage, bool) {
	if memoryStore == nil || memoryStore.ExternalID == "" {
		return nil, false
	}
	raw, err := json.Marshal(memoryStoreRuntimeSource{
		Type:          "memory_store",
		MemoryStoreID: memoryStore.ExternalID,
		Access:        memoryStore.Access,
		Description:   memoryStore.Description,
		Instructions:  memoryStore.Instructions,
		MountPath:     memoryStore.MountPath,
		Name:          memoryStore.Name,
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
