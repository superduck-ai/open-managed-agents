package sessions

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type sessionResourceEnvironmentConfig struct {
	Type string `json:"type"`
}

func validateEnvironmentSessionResources(environment db.Environment, resources []normalizedSessionResource) error {
	var config sessionResourceEnvironmentConfig
	if err := json.Unmarshal(environment.Config, &config); err != nil {
		return invalidResourceEnvironmentConfig(err)
	}
	if config.Type != "self_hosted" {
		return nil
	}
	for _, resource := range resources {
		switch resource.resource.ResourceType {
		case sessionresource.FileType, sessionresource.GitRepositoryType:
			return unsupportedSelfHostedResource(environment.ExternalID, resource.resource.ResourceType)
		}
	}
	return nil
}

func selfHostedMemoryCredentialRequired(environment db.Environment, resources []normalizedSessionResource) bool {
	var config sessionResourceEnvironmentConfig
	if json.Unmarshal(environment.Config, &config) != nil || config.Type != "self_hosted" {
		return false
	}
	for _, resource := range resources {
		if resource.resource.ResourceType == sessionresource.MemoryStoreType {
			return true
		}
	}
	return false
}
