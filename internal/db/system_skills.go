package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/superduck-ai/yourbatis"
)

var (
	ErrSystemSkillDenied   = errors.New("system skill administration denied")
	ErrSystemSkillRevision = errors.New("system skill revision changed")
)

type SystemSkillImport struct {
	ActorUUID        string
	Skill            SystemSkill
	Version          SystemSkillVersion
	ExpectedRevision int64
	Publish          bool
}

type SystemSkillChange struct {
	ActorUUID string
	SkillID   string
	Revision  int64
	Action    string
	Version   string
}

func (d *DB) ListSystemSkills(ctx context.Context, query SystemSkillQuery) ([]SystemSkill, error) {
	return NewSystemSkillMapper(d.mapperDB).List(ctx, query)
}

func (d *DB) GetSystemSkill(ctx context.Context, id string) (SystemSkill, error) {
	skill, err := NewSystemSkillMapper(d.mapperDB).Find(ctx, id, false)
	return skill, mapNoRows(err)
}

func (d *DB) SystemSkillVersions(ctx context.Context, skillUUID string, limit, offset int) ([]SystemSkillVersion, error) {
	return NewSystemSkillMapper(d.mapperDB).Versions(ctx, skillUUID, limit, offset)
}

func (d *DB) SystemSkillVersion(ctx context.Context, skillUUID, version string) (SystemSkillVersion, error) {
	row, err := NewSystemSkillMapper(d.mapperDB).Version(ctx, skillUUID, version)
	return row, mapNoRows(err)
}

func (d *DB) SystemSkillEvents(ctx context.Context, skillUUID string, limit, offset int) ([]SystemSkillEvent, error) {
	return NewSystemSkillMapper(d.mapperDB).Events(ctx, skillUUID, limit, offset)
}

func (d *DB) HasSystemSkillArchive(ctx context.Context, key string) (bool, error) {
	return NewSystemSkillMapper(d.mapperDB).HasArchive(ctx, key)
}

func authorizeSystemSkillWrite(ctx context.Context, executor yourbatis.Executor, actor string) error {
	granted, err := NewPlatformAdminMapper(executor).LockGrant(ctx, actor)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !granted) {
		return ErrSystemSkillDenied
	}
	return err
}

func (d *DB) ImportSystemSkill(ctx context.Context, input SystemSkillImport) (SystemSkill, bool, error) {
	var result SystemSkill
	duplicate := false
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := authorizeSystemSkillWrite(ctx, executor, input.ActorUUID); err != nil {
			return err
		}
		mapper := NewSystemSkillMapper(executor)
		inserted, err := mapper.InsertSkill(ctx, input.Skill)
		if err != nil {
			return err
		}
		skill, err := mapper.Find(ctx, input.Skill.ExternalID, true)
		if err != nil {
			return err
		}
		previous, err := mapper.Version(ctx, skill.UUID, input.Version.Version)
		if err == nil {
			if previous.SHA256 != input.Version.SHA256 {
				return ErrVersionConflict
			}
			result, duplicate = skill, true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if inserted == 0 && skill.Revision != input.ExpectedRevision {
			return ErrSystemSkillRevision
		}
		if inserted == 1 && input.ExpectedRevision != 0 {
			return ErrSystemSkillRevision
		}
		if inserted == 0 {
			versions, err := mapper.Versions(ctx, skill.UUID, 1, 0)
			if err != nil {
				return err
			}
			if len(versions) > 0 && versions[0].Name != input.Version.Name {
				return ErrInvalidState
			}
		}
		version := input.Version
		version.SkillUUID, version.SkillExternalID = skill.UUID, skill.ExternalID
		if err := mapper.InsertVersion(ctx, version); err != nil {
			return err
		}
		if input.Publish {
			if _, err := mapper.Publish(ctx, skill.UUID, version.Version); err != nil {
				return err
			}
			if skill.LatestVersion == nil {
				skill.LatestVersion, skill.Listed = new(version.Version), true
			}
		}
		if err := saveSystemSkill(ctx, mapper, skill, input.ActorUUID, "import_draft", version.Version); err != nil {
			return err
		}
		if input.Publish {
			if err := mapper.Record(ctx, SystemSkillEvent{SkillUUID: skill.UUID, ActorUUID: input.ActorUUID, Action: "publish", Version: version.Version}); err != nil {
				return err
			}
		}
		result, err = mapper.Find(ctx, skill.ExternalID, false)
		return err
	})
	return result, duplicate, err
}

func saveSystemSkill(ctx context.Context, mapper SystemSkillMapper, skill SystemSkill, actor, action, version string) error {
	changed, err := mapper.UpdateSkill(ctx, skill)
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrSystemSkillRevision
	}
	return mapper.Record(ctx, SystemSkillEvent{SkillUUID: skill.UUID, ActorUUID: actor, Action: action, Version: version})
}

func (d *DB) ChangeSystemSkill(ctx context.Context, input SystemSkillChange) (SystemSkill, error) {
	var result SystemSkill
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := authorizeSystemSkillWrite(ctx, executor, input.ActorUUID); err != nil {
			return err
		}
		mapper := NewSystemSkillMapper(executor)
		skill, err := mapper.Find(ctx, input.SkillID, true)
		if err != nil {
			return mapNoRows(err)
		}
		if skill.Revision != input.Revision {
			return ErrSystemSkillRevision
		}
		if err := changeSystemSkill(ctx, mapper, &skill, input); err != nil {
			return err
		}
		if err := saveSystemSkill(ctx, mapper, skill, input.ActorUUID, input.Action, input.Version); err != nil {
			return err
		}
		result, err = mapper.Find(ctx, skill.ExternalID, false)
		return err
	})
	return result, err
}

func changeSystemSkill(ctx context.Context, mapper SystemSkillMapper, skill *SystemSkill, input SystemSkillChange) error {
	switch input.Action {
	case "publish", "set_default":
		version, err := mapper.Version(ctx, skill.UUID, input.Version)
		if err != nil {
			return mapNoRows(err)
		}
		if input.Action == "set_default" && version.State != "published" {
			return ErrInvalidState
		}
		if input.Action == "publish" {
			if _, err := mapper.Publish(ctx, skill.UUID, version.Version); err != nil {
				return err
			}
		}
		if skill.LatestVersion == nil {
			skill.LatestVersion, skill.Listed = new(version.Version), true
		}
		if input.Action == "set_default" {
			skill.LatestVersion = new(version.Version)
		}
	case "list":
		if skill.LatestVersion == nil {
			return ErrInvalidState
		}
		version, err := mapper.Version(ctx, skill.UUID, *skill.LatestVersion)
		if err != nil {
			return err
		}
		if version.State != "published" {
			return ErrInvalidState
		}
		skill.Listed = true
	case "unlist":
		skill.Listed = false
	default:
		return ErrInvalidState
	}
	return nil
}
