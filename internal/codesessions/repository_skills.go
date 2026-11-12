package codesessions

import (
	"context"
	"encoding/json"
	"path"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type RepositorySkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

type repositorySkillResolver interface {
	RepositorySkills(context.Context, db.CodeSession, []string) ([]RepositorySkill, error)
}

func repositorySkillRoots(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var config struct {
		Protocol string   `json:"worker_protocol"`
		Tools    []string `json:"runtime_tools"`
		Sources  []struct {
			Type      string `json:"type"`
			MountPath string `json:"mount_path"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if config.Protocol != "dsr1" || !slices.Contains(config.Tools, "Read") {
		return nil, nil
	}
	var roots []string
	for _, source := range config.Sources {
		if source.Type != "git_repository" {
			continue
		}
		if !path.IsAbs(source.MountPath) || path.Clean(source.MountPath) != source.MountPath {
			return nil, errRepositorySkillRoot
		}
		roots = append(roots, source.MountPath)
	}
	return roots, nil
}

func mergeRepositorySkills(raw json.RawMessage, discovered []RepositorySkill) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var attached []json.RawMessage
	if value := fields["runtime_skills"]; len(value) > 0 {
		if err := json.Unmarshal(value, &attached); err != nil {
			return nil, err
		}
	}
	for _, skill := range discovered {
		value, err := json.Marshal(skill)
		if err != nil {
			return nil, err
		}
		attached = append(attached, value)
	}
	value, err := json.Marshal(attached)
	if err != nil {
		return nil, err
	}
	fields["runtime_skills"] = value
	return json.Marshal(fields)
}

func (s *Service) prepareRepositoryContext(ctx context.Context, session db.Session, codeSessionID string) error {
	record, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return err
	}
	if !found || record.OrganizationUUID != session.OrganizationUUID || record.WorkspaceUUID != session.WorkspaceUUID || record.SessionUUID != session.UUID {
		return db.ErrNotFound
	}
	var metadata struct {
		Config json.RawMessage `json:"config"`
	}
	if err = json.Unmarshal(record.Metadata, &metadata); err != nil {
		return err
	}
	roots, err := repositorySkillRoots(metadata.Config)
	if err != nil || len(roots) == 0 {
		return err
	}
	scope := db.DSHScope{CodeSessionID: record.ExternalID, SessionID: record.SessionExternalID, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, CodeSessionUUID: record.UUID, Epoch: record.CurrentWorkerEpoch}
	frozen, err := s.db.GetDSHContextSnapshot(ctx, scope)
	if err != nil || len(frozen) > 0 {
		return err
	}
	resolver, ok := s.sandboxProvisioner.(repositorySkillResolver)
	if !ok {
		return errSandboxUnavailable
	}
	skills, err := resolver.RepositorySkills(ctx, record, roots)
	if err != nil {
		return err
	}
	metadata.Config, err = mergeRepositorySkills(metadata.Config, skills)
	if err != nil {
		return err
	}
	record.Metadata, err = json.Marshal(metadata)
	if err != nil {
		return err
	}
	candidate, err := s.dshContextCandidate(ctx, record)
	if err != nil {
		return err
	}
	receipt, err := s.db.FreezeDSHContextWithReceipt(ctx, scope, candidate)
	if err == nil {
		s.observeOwnedRepositoryFreeze(ctx, scope, candidate, receipt, roots, len(skills))
	}
	return err
}
