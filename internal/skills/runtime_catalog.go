package skills

import (
	"encoding/json"
	"fmt"
)

type RuntimeCatalogEntry struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Version           string   `json:"version"`
	Path              string   `json:"path"`
	Source            string   `json:"source,omitempty"`
	SkillID           string   `json:"skill_id,omitempty"`
	VersionUUID       string   `json:"version_uuid,omitempty"`
	SHA256            string   `json:"sha256,omitempty"`
	RequestedVersions []string `json:"requested_versions,omitempty"`
}

type RuntimeAgentSkills struct {
	AgentID      string                `json:"agent_id"`
	AgentVersion int                   `json:"agent_version"`
	Skills       []RuntimeCatalogEntry `json:"skills"`
}

func RuntimeCatalogs(snapshot json.RawMessage, installations []RuntimeSkill) (map[string]RuntimeAgentSkills, error) {
	scopes, err := runtimeSkillScopes(snapshot)
	if err != nil {
		return nil, err
	}
	catalogs := make(map[string]RuntimeAgentSkills, len(scopes))
	for _, scope := range scopes {
		catalogs[scope.binding.Scope] = RuntimeAgentSkills{AgentID: scope.binding.AgentID, AgentVersion: scope.binding.AgentVersion, Skills: []RuntimeCatalogEntry{}}
	}
	for _, skill := range installations {
		bindings := skill.Bindings
		if len(bindings) == 0 {
			bindings = []RuntimeSkillBinding{{RequestedVersion: skill.RequestedVersion}}
		}
		entries := map[string]*RuntimeCatalogEntry{}
		for _, binding := range bindings {
			authority, ok := catalogs[binding.Scope]
			if !ok || authority.AgentID != binding.AgentID || authority.AgentVersion != binding.AgentVersion {
				return nil, fmt.Errorf("Skill binding scope identity mismatch")
			}
			entry := entries[binding.Scope]
			if entry == nil {
				entry = &RuntimeCatalogEntry{Name: skill.Name, Description: skill.Description, Version: skill.Version, Path: "/root/.claude/skills/" + skill.MountDirectory() + "/SKILL.md", Source: skill.Source, SkillID: skill.SkillID, VersionUUID: skill.VersionUUID, SHA256: skill.SHA256}
				entries[binding.Scope] = entry
			}
			entry.RequestedVersions = append(entry.RequestedVersions, binding.RequestedVersion)
		}
		for scope, entry := range entries {
			catalog := catalogs[scope]
			catalog.Skills = append(catalog.Skills, *entry)
			catalogs[scope] = catalog
		}
	}
	return catalogs, nil
}
