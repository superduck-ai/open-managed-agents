package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/superduck-ai/yourbatis"
)

var ErrSystemSkillNameMismatch = errors.New("system skill name does not match")

type SystemSkillImport struct {
	Skill   SystemSkill
	Version SystemSkillVersion
}

type SystemSkillChange struct {
	IconKey string
	SkillID string
	Action  string
}

type ListSystemSkillsPageParams struct {
	Limit  int
	Offset int
}

type ListSystemSkillVersionsPageParams struct {
	OldestFirst     bool
	SkillExternalID string
	Limit           int
	Offset          int
}

func (d *DB) ListSystemSkills(ctx context.Context, query SystemSkillQuery) ([]SystemSkill, error) {
	return NewSystemSkillMapper(d.mapperDB).List(ctx, query)
}

func (d *DB) ListListedSystemSkillsPage(ctx context.Context, params ListSystemSkillsPageParams) ([]SystemSkill, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	rows, err := NewSystemSkillMapper(d.mapperDB).ListListed(ctx, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > params.Limit
	if hasMore {
		rows = rows[:params.Limit]
	}
	return rows, hasMore, nil
}

func (d *DB) CountListedSystemSkills(ctx context.Context) (int, error) {
	return NewSystemSkillMapper(d.mapperDB).CountListed(ctx)
}

func (d *DB) GetSystemSkill(ctx context.Context, id string) (SystemSkill, error) {
	skill, err := NewSystemSkillMapper(d.mapperDB).Find(ctx, id, false)
	return skill, mapNoRows(err)
}

func (d *DB) GetAvailableSystemSkill(ctx context.Context, externalID string) (SystemSkill, error) {
	skill, err := NewSystemSkillMapper(d.mapperDB).FindAvailable(ctx, externalID)
	return skill, mapNoRows(err)
}

func (d *DB) ListSystemSkillVersionsPage(ctx context.Context, params ListSystemSkillVersionsPageParams) ([]SystemSkillVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	mapper := NewSystemSkillMapper(d.mapperDB)
	skillUUID, err := mapper.FindUUIDByExternalID(ctx, params.SkillExternalID)
	if err != nil {
		return nil, false, mapNoRows(err)
	}
	rows, err := mapper.Versions(ctx, skillUUID, params.Limit+1, params.Offset, params.OldestFirst)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > params.Limit
	if hasMore {
		rows = rows[:params.Limit]
	}
	return rows, hasMore, nil
}

func (d *DB) SystemSkillVersions(ctx context.Context, skillUUID string, limit, offset int) ([]SystemSkillVersion, error) {
	return NewSystemSkillMapper(d.mapperDB).Versions(ctx, skillUUID, limit, offset, false)
}

func (d *DB) GetSystemSkillVersion(ctx context.Context, skillExternalID, version string) (SystemSkillVersion, error) {
	if version == "latest" {
		skill, err := d.GetAvailableSystemSkill(ctx, skillExternalID)
		if err != nil {
			return SystemSkillVersion{}, err
		}
		if skill.LatestVersion == nil || strings.TrimSpace(*skill.LatestVersion) == "" {
			return SystemSkillVersion{}, ErrNotFound
		}
		version = *skill.LatestVersion
	}
	row, err := NewSystemSkillMapper(d.mapperDB).FindVersionByExternalID(ctx, skillExternalID, version)
	return row, mapNoRows(err)
}

func (d *DB) HasSystemSkillArchive(ctx context.Context, key string) (bool, error) {
	return NewSystemSkillMapper(d.mapperDB).HasArchive(ctx, key)
}

func (tx *PlatformAdminTx) ImportSystemSkill(ctx context.Context, input SystemSkillImport) (SystemSkill, bool, error) {
	mapper := NewSystemSkillMapper(tx.executor)
	inserted, err := mapper.InsertSkill(ctx, input.Skill)
	if err != nil {
		return SystemSkill{}, false, err
	}
	skill, err := mapper.Find(ctx, input.Skill.ExternalID, true)
	if errors.Is(err, sql.ErrNoRows) {
		return SystemSkill{}, false, ErrInvalidState
	}
	if err != nil {
		return SystemSkill{}, false, err
	}
	previous, err := mapper.Version(ctx, skill.UUID, input.Version.Version)
	if errors.Is(err, sql.ErrNoRows) {
		previous, err = mapper.VersionBySHA256(ctx, skill.UUID, input.Version.SHA256)
	}
	duplicate := err == nil
	if err == nil {
		if previous.SHA256 != input.Version.SHA256 {
			return SystemSkill{}, false, ErrVersionConflict
		}
		if inserted == 0 {
			return skill, true, nil
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SystemSkill{}, false, err
	}
	if inserted == 0 {
		versions, err := mapper.Versions(ctx, skill.UUID, 1, 0, false)
		if err != nil {
			return SystemSkill{}, false, err
		}
		if len(versions) > 0 && versions[0].Name != input.Version.Name {
			return SystemSkill{}, false, ErrSystemSkillNameMismatch
		}
	}
	version := input.Version
	if duplicate {
		version = previous
	} else {
		version.SkillUUID, version.SkillExternalID = skill.UUID, skill.ExternalID
		if err := mapper.InsertVersion(ctx, version); err != nil {
			return SystemSkill{}, false, err
		}
	}
	skill.LatestVersion = new(version.Version)
	if input.Skill.IconKey != "" {
		skill.IconKey = input.Skill.IconKey
	}
	if _, err := mapper.UpdateSkill(ctx, skill); err != nil {
		return SystemSkill{}, false, err
	}
	result, err := mapper.Find(ctx, skill.ExternalID, false)
	return result, duplicate, err
}

func (tx *PlatformAdminTx) ChangeSystemSkill(ctx context.Context, input SystemSkillChange) (SystemSkill, error) {
	mapper := NewSystemSkillMapper(tx.executor)
	skill, err := mapper.Find(ctx, input.SkillID, true)
	if err != nil {
		return SystemSkill{}, mapNoRows(err)
	}
	switch input.Action {
	case "delete":
		if skill.Bundled {
			if skill.BundledVersion == nil || (skill.LatestVersion != nil && *skill.LatestVersion == *skill.BundledVersion) {
				return SystemSkill{}, ErrBundledSkillReadOnly
			}
			skill.LatestVersion = new(*skill.BundledVersion)
			if _, err := mapper.UpdateSkill(ctx, skill); err != nil {
				return SystemSkill{}, err
			}
			return mapper.Find(ctx, skill.ExternalID, false)
		}
		if _, err := mapper.DeleteSkill(ctx, skill); err != nil {
			return SystemSkill{}, err
		}
		return skill, nil
	case "set_icon":
		skill.IconKey = input.IconKey
	case "list":
		if skill.LatestVersion == nil {
			return SystemSkill{}, ErrInvalidState
		}
		skill.Listed = true
	case "unlist":
		skill.Listed = false
	default:
		return SystemSkill{}, ErrInvalidState
	}
	if _, err := mapper.UpdateSkill(ctx, skill); err != nil {
		return SystemSkill{}, err
	}
	return mapper.Find(ctx, skill.ExternalID, false)
}

func (d *DB) SyncBundledSkill(ctx context.Context, skill SystemSkill, version SystemSkillVersion) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSystemSkillMapper(executor)
		skill.LatestVersion = new(version.Version)
		stored, err := mapper.RegisterBundled(ctx, skill)
		if err != nil {
			return err
		}
		previous, err := mapper.Version(ctx, stored.UUID, version.Version)
		if err == nil {
			if previous.SHA256 != version.SHA256 {
				return ErrVersionConflict
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			version.SkillUUID, version.SkillExternalID = stored.UUID, stored.ExternalID
			return mapper.InsertVersion(ctx, version)
		} else {
			return err
		}
		return nil
	})
}
