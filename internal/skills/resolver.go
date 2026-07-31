package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

func NewRuntimeResolver(database *db.DB) *RuntimeResolver {
	return &RuntimeResolver{db: database}
}

func (r *RuntimeResolver) ResolveAgentSnapshot(ctx context.Context, workspaceUUID string, snapshot json.RawMessage) ([]RuntimeSkill, error) {
	if r == nil {
		return nil, nil
	}
	refs, err := runtimeSkillRefs(snapshot)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}

	resolved := make([]RuntimeSkill, 0, len(refs))
	seenRefs := map[string]struct{}{}
	seenResolved := map[string]struct{}{}
	dirs := map[string]string{}
	for _, ref := range refs {
		ref.Type = strings.TrimSpace(ref.Type)
		ref.SkillID = strings.TrimSpace(ref.SkillID)
		ref.Version = firstNonEmpty(ref.Version, "latest")
		refKey := ref.Type + "\x00" + ref.SkillID + "\x00" + ref.Version
		if _, ok := seenRefs[refKey]; ok {
			continue
		}
		seenRefs[refKey] = struct{}{}

		skill, err := r.resolveRef(ctx, workspaceUUID, ref)
		if err != nil {
			return nil, err
		}
		resolvedKey := skill.Source + "\x00" + skill.SkillID + "\x00" + skill.Version
		if _, ok := seenResolved[resolvedKey]; ok {
			continue
		}
		dirKey := strings.TrimSpace(skill.Directory)
		if previous, ok := dirs[dirKey]; ok && previous != resolvedKey {
			return nil, fmt.Errorf("skill install directory %q is used by multiple skills", dirKey)
		}
		dirs[dirKey] = resolvedKey
		seenResolved[resolvedKey] = struct{}{}
		resolved = append(resolved, skill)
	}
	return resolved, nil
}

func runtimeSkillRefs(snapshot json.RawMessage) ([]runtimeSkillRef, error) {
	object := map[string]json.RawMessage{}
	if len(snapshot) == 0 || strings.TrimSpace(string(snapshot)) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(snapshot, &object); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	raw, ok := object["skills"]
	if !ok || len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var refs []runtimeSkillRef
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, fmt.Errorf("decode agent skills: %w", err)
	}
	for i, ref := range refs {
		if strings.TrimSpace(ref.Type) != "anthropic" && strings.TrimSpace(ref.Type) != "custom" {
			return nil, fmt.Errorf("skill %d type must be anthropic or custom", i)
		}
		if strings.TrimSpace(ref.SkillID) == "" {
			return nil, fmt.Errorf("skill %d id must be non-empty", i)
		}
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
			if ref.Version == "latest" {
				return RuntimeSkill{}, fmt.Errorf("built-in skill not found: %s", ref.SkillID)
			}
			return RuntimeSkill{}, fmt.Errorf("built-in skill version not found: %s@%s", ref.SkillID, ref.Version)
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
			if ref.Version == "latest" {
				return RuntimeSkill{}, fmt.Errorf("custom skill latest version not found: %s", ref.SkillID)
			}
			return RuntimeSkill{}, fmt.Errorf("custom skill version not found: %s@%s", ref.SkillID, ref.Version)
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
