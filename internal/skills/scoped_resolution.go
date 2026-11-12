package skills

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
)

type RuntimeSkillBinding struct {
	Scope            string
	AgentID          string
	AgentVersion     int
	RequestedVersion string
}

type runtimeSkillScope struct {
	binding RuntimeSkillBinding
	refs    []runtimeSkillRef
}

func (s RuntimeSkill) MountDirectory() string {
	if s.InstallDirectory != "" {
		return s.InstallDirectory
	}
	return s.Directory
}

func runtimeSkillScopes(snapshot json.RawMessage) ([]runtimeSkillScope, error) {
	if len(snapshot) == 0 || string(snapshot) == "null" {
		return nil, nil
	}
	var roster runtimeSkillSnapshot
	if err := jsonv2.Unmarshal(snapshot, &roster, jsonv2.MatchCaseInsensitiveNames(true), json.MatchCaseSensitiveDelimiter(true)); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	scopes := []runtimeSkillScope{{refs: roster.Skills}}
	if roster.Multiagent != nil {
		children := roster.Multiagent.Agents
		if roster.Multiagent.Type == "multiagent_20261001" {
			children = nil
			for _, setting := range []*runtimeSkillDelegation{roster.Multiagent.Subagents, roster.Multiagent.Workflows} {
				if setting != nil && setting.Type == "enabled" {
					children = append(children, setting.PredefinedAgents...)
				}
			}
		}
		seenNames := map[string]bool{}
		for i, child := range children {
			name := child.Name
			if name != "" && seenNames[name] {
				return nil, fmt.Errorf("duplicate Skill agent scope %q", name)
			}
			seenNames[name] = true
			if name == "" {
				name = "@child-" + strconv.Itoa(i)
			}
			scopes = append(scopes, runtimeSkillScope{binding: RuntimeSkillBinding{Scope: name, AgentID: child.ID, AgentVersion: child.Version}, refs: child.Skills})
		}
	}
	for _, scope := range scopes {
		for i, ref := range scope.refs {
			if strings.TrimSpace(ref.Type) != "anthropic" && strings.TrimSpace(ref.Type) != "custom" {
				return nil, fmt.Errorf("skill %d type must be anthropic or custom", i)
			}
			if strings.TrimSpace(ref.SkillID) == "" {
				return nil, fmt.Errorf("skill %d id must be non-empty", i)
			}
		}
	}
	return scopes, nil
}

func (r *RuntimeResolver) resolveScopedSkills(ctx context.Context, workspace string, scopes []runtimeSkillScope) ([]RuntimeSkill, error) {
	var resolved []RuntimeSkill
	seen := map[string]int{}
	cache := map[string]RuntimeSkill{}
	for _, scope := range scopes {
		for _, ref := range scope.refs {
			ref.Type = strings.TrimSpace(ref.Type)
			ref.SkillID = strings.TrimSpace(ref.SkillID)
			ref.Version = firstNonEmpty(ref.Version, "latest")
			refKey := ref.Type + "\x00" + ref.SkillID + "\x00" + ref.Version
			skill, ok := cache[refKey]
			if !ok {
				var err error
				skill, err = r.resolveRef(ctx, workspace, ref)
				if err != nil {
					return nil, err
				}
				cache[refKey] = skill
			}
			key := skill.Source + "\x00" + skill.SkillID + "\x00" + skill.VersionUUID
			index, ok := seen[key]
			if !ok {
				index = len(resolved)
				seen[key] = index
				resolved = append(resolved, skill)
			}
			binding := scope.binding
			binding.RequestedVersion = ref.Version
			exists := false
			for _, previous := range resolved[index].Bindings {
				exists = exists || previous == binding
			}
			if !exists {
				resolved[index].Bindings = append(resolved[index].Bindings, binding)
			}
			if ref.Version == "latest" {
				resolved[index].RequestedVersion = "latest"
			}
		}
	}
	if err := allocateSkillDirectories(resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func allocateSkillDirectories(skills []RuntimeSkill) error {
	for i := range skills {
		for j := 0; j < i; j++ {
			first, second := &skills[i], &skills[j]
			sameSkill := first.Source == second.Source && first.SkillID == second.SkillID
			if !sameSkill && first.Directory != second.Directory && first.Name != second.Name {
				continue
			}
			for _, a := range first.Bindings {
				for _, b := range second.Bindings {
					if a.Scope == b.Scope {
						return fmt.Errorf("skill install directory or name %q is used by multiple skills in one agent scope", first.Directory)
					}
				}
			}
			if first.Directory != second.Directory {
				continue
			}
			if first.Source != second.Source || first.SkillID != second.SkillID {
				return fmt.Errorf("skill install directory %q is used by multiple skills", first.Directory)
			}
			first.InstallDirectory = "version-" + strings.ReplaceAll(first.VersionUUID, "-", "")
			second.InstallDirectory = "version-" + strings.ReplaceAll(second.VersionUUID, "-", "")
		}
	}
	seen := map[string]bool{}
	for _, skill := range skills {
		directory := skill.MountDirectory()
		if seen[directory] {
			return fmt.Errorf("skill install directory %q is used by multiple skills", directory)
		}
		seen[directory] = true
	}
	return nil
}
