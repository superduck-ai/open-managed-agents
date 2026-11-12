package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type RuntimeResolver struct {
	db *db.DB
}

type RuntimeSkill struct {
	Source           string
	SkillID          string
	VersionUUID      string
	RequestedVersion string
	Version          string
	Directory        string
	InstallDirectory string
	Bindings         []RuntimeSkillBinding
	Name             string
	Description      string
	S3Bucket         string
	S3Key            string
	SHA256           string
	SizeBytes        int64
}

type runtimeSkillRef struct {
	Type    string `json:"type"`
	SkillID string `json:"skill_id"`
	Version string `json:"version"`
}

type runtimeSkillSnapshot struct {
	Skills     []runtimeSkillRef `json:"skills"`
	Multiagent *struct {
		Type      string                  `json:"type"`
		Agents    []runtimeSkillAgent     `json:"agents,case:strict"`
		Subagents *runtimeSkillDelegation `json:"subagents,case:strict"`
		Workflows *runtimeSkillDelegation `json:"workflows,case:strict"`
	} `json:"multiagent,case:strict"`
}

func NewRuntimeResolver(database *db.DB) *RuntimeResolver {
	return &RuntimeResolver{db: database}
}

func (r *RuntimeResolver) ResolveAgentSnapshot(ctx context.Context, workspaceUUID string, snapshot json.RawMessage) ([]RuntimeSkill, error) {
	if r == nil {
		return nil, nil
	}
	scopes, err := runtimeSkillScopes(snapshot)
	if err != nil {
		return nil, err
	}
	return r.resolveScopedSkills(ctx, workspaceUUID, scopes)
}

func runtimeSkillRefs(snapshot json.RawMessage) ([]runtimeSkillRef, error) {
	scopes, err := runtimeSkillScopes(snapshot)
	if err != nil {
		return nil, err
	}
	var refs []runtimeSkillRef
	for _, scope := range scopes {
		refs = append(refs, scope.refs...)
	}
	return refs, nil
}

func (r *RuntimeResolver) resolveRef(ctx context.Context, workspaceUUID string, ref runtimeSkillRef) (RuntimeSkill, error) {
	switch ref.Type {
	case "anthropic":
		return r.resolveBuiltin(ctx, ref)
	case "custom":
		return r.resolveCustom(ctx, workspaceUUID, ref)
	default:
		return RuntimeSkill{}, fmt.Errorf("unsupported skill type %q", ref.Type)
	}
}

func (r *RuntimeResolver) resolveBuiltin(ctx context.Context, ref runtimeSkillRef) (RuntimeSkill, error) {
	if r.db == nil {
		return RuntimeSkill{}, errors.New("built-in skill resolver is unavailable")
	}
	record, err := r.db.GetBuiltinSkillVersion(ctx, ref.SkillID, ref.Version)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return RuntimeSkill{}, bindingReferenceError(ref, err)
		}
		return RuntimeSkill{}, err
	}
	return RuntimeSkill{
		Source:           "anthropic",
		SkillID:          record.SkillExternalID,
		VersionUUID:      record.UUID,
		RequestedVersion: ref.Version,
		Version:          record.Version,
		Directory:        record.Directory,
		Name:             firstNonEmpty(record.Name, record.Directory, record.SkillExternalID),
		Description:      record.Description,
		S3Bucket:         record.S3Bucket,
		S3Key:            record.S3Key,
		SHA256:           record.SHA256,
		SizeBytes:        record.SizeBytes,
	}, nil
}

func (r *RuntimeResolver) resolveCustom(ctx context.Context, workspaceUUID string, ref runtimeSkillRef) (RuntimeSkill, error) {
	if r.db == nil {
		return RuntimeSkill{}, errors.New("custom skill resolver is unavailable")
	}
	var record db.SkillVersion
	var err error
	if ref.Version == "latest" {
		record, err = r.db.GetLatestSkillVersion(ctx, workspaceUUID, ref.SkillID)
	} else {
		record, err = r.db.GetSkillVersion(ctx, workspaceUUID, ref.SkillID, ref.Version)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return RuntimeSkill{}, bindingReferenceError(ref, err)
		}
		return RuntimeSkill{}, err
	}
	return RuntimeSkill{
		Source:           "custom",
		SkillID:          ref.SkillID,
		VersionUUID:      record.UUID,
		RequestedVersion: ref.Version,
		Version:          record.Version,
		Directory:        record.Directory,
		Name:             record.Name,
		Description:      record.Description,
		S3Bucket:         record.S3Bucket,
		S3Key:            record.S3Key,
		SHA256:           record.SHA256,
		SizeBytes:        record.SizeBytes,
	}, nil
}

type runtimeSkillAgent struct {
	ID      string            `json:"id"`
	Version int               `json:"version"`
	Name    string            `json:"name"`
	Skills  []runtimeSkillRef `json:"skills"`
}

type runtimeSkillDelegation struct {
	Type             string              `json:"type"`
	PredefinedAgents []runtimeSkillAgent `json:"predefined_agents,case:strict"`
}
