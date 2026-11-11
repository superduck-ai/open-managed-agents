package db

import (
	"context"
	"database/sql"
	"errors"
)

var (
	ErrSystemSkillRevision     = errors.New("system skill revision changed")
	ErrSystemSkillNameMismatch = errors.New("system skill name does not match")
)

type SystemSkillImport struct {
	ActorUUID        string
	Skill            SystemSkill
	Version          SystemSkillVersion
	ExpectedRevision int64
	Publish          bool
	Ready            bool
}

type SystemSkillChange struct {
	IconKey   string
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
	return NewSystemSkillMapper(d.mapperDB).Versions(ctx, skillUUID, true, limit, offset)
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
	if (inserted == 0 && skill.Revision != input.ExpectedRevision) || (inserted == 1 && input.ExpectedRevision != 0) {
		return SystemSkill{}, false, ErrSystemSkillRevision
	}
	if inserted == 0 {
		versions, err := mapper.Versions(ctx, skill.UUID, false, 1, 0)
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
		if input.Publish && version.State == "published" {
			skill.LatestVersion, skill.Listed = new(version.Version), true
		}
	} else {
		version.SkillUUID, version.SkillExternalID = skill.UUID, skill.ExternalID
		if err := mapper.InsertVersion(ctx, version); err != nil {
			return SystemSkill{}, false, err
		}
	}
	published := false
	if input.Ready {
		published, err = activateSystemSkillVersion(ctx, mapper, &skill, version)
		if err != nil {
			return SystemSkill{}, false, err
		}
		if input.Skill.IconKey != "" {
			skill.IconKey = input.Skill.IconKey
		}
	}
	if input.Publish {
		published, err = publishSystemSkill(ctx, mapper, &skill, version.Version)
		if err != nil {
			return SystemSkill{}, false, err
		}
	}
	action := "import_draft"
	if input.Ready {
		action = "import"
	}
	if err := saveSystemSkill(ctx, mapper, skill, input.ActorUUID, action, version.Version); err != nil {
		return SystemSkill{}, false, err
	}
	if published {
		if err := mapper.Record(ctx, SystemSkillEvent{SkillUUID: skill.UUID, ActorUUID: input.ActorUUID, Action: "publish", Version: version.Version}); err != nil {
			return SystemSkill{}, false, err
		}
	}
	result, err := mapper.Find(ctx, skill.ExternalID, false)
	return result, duplicate, err
}

func activateSystemSkillVersion(ctx context.Context, mapper SystemSkillMapper, skill *SystemSkill, version SystemSkillVersion) (bool, error) {
	published := version.State != "published"
	if published {
		changed, err := mapper.Publish(ctx, skill.UUID, version.Version)
		if err != nil {
			return false, err
		}
		if changed != 1 {
			return false, ErrInvalidState
		}
	}
	if skill.LatestVersion == nil || skill.Bundled {
		skill.LatestVersion = new(version.Version)
	}
	return published, nil
}

func saveSystemSkill(ctx context.Context, mapper SystemSkillMapper, skill SystemSkill, actor, action, version string) error {
	var changed int64
	var err error
	if action == "delete" && !skill.Bundled {
		changed, err = mapper.DeleteSkill(ctx, skill)
	} else {
		changed, err = mapper.UpdateSkill(ctx, skill)
	}
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrSystemSkillRevision
	}
	if action == "delete" && skill.Bundled {
		action, version = "restore_bundled", *skill.BundledVersion
	}
	return mapper.Record(ctx, SystemSkillEvent{SkillUUID: skill.UUID, ActorUUID: actor, Action: action, Version: version})
}

func (tx *PlatformAdminTx) ChangeSystemSkill(ctx context.Context, input SystemSkillChange) (SystemSkill, error) {
	mapper := NewSystemSkillMapper(tx.executor)
	skill, err := mapper.Find(ctx, input.SkillID, true)
	if err != nil {
		return SystemSkill{}, mapNoRows(err)
	}
	if skill.Revision != input.Revision {
		return SystemSkill{}, ErrSystemSkillRevision
	}
	changed, err := changeSystemSkill(ctx, mapper, &skill, input)
	if err != nil {
		return SystemSkill{}, err
	}
	if !changed {
		return skill, nil
	}
	if err := saveSystemSkill(ctx, mapper, skill, input.ActorUUID, input.Action, input.Version); err != nil {
		return SystemSkill{}, err
	}
	if input.Action == "delete" && !skill.Bundled {
		return skill, nil
	}
	return mapper.Find(ctx, skill.ExternalID, false)
}

func publishSystemSkill(ctx context.Context, mapper SystemSkillMapper, skill *SystemSkill, version string) (bool, error) {
	current, err := mapper.Version(ctx, skill.UUID, version)
	if err != nil {
		return false, mapNoRows(err)
	}
	if current.State == "published" {
		return false, nil
	}
	changed, err := mapper.Publish(ctx, skill.UUID, version)
	if err != nil {
		return false, err
	}
	if changed != 1 {
		return false, ErrInvalidState
	}
	if skill.LatestVersion == nil {
		skill.LatestVersion, skill.Listed = new(version), true
	}
	return true, nil
}

func changeSystemSkill(ctx context.Context, mapper SystemSkillMapper, skill *SystemSkill, input SystemSkillChange) (bool, error) {
	switch input.Action {
	case "delete":
		if skill.Bundled {
			if skill.BundledVersion == nil || (skill.LatestVersion != nil && *skill.LatestVersion == *skill.BundledVersion) {
				return false, ErrBundledSkillReadOnly
			}
			input.Action, input.Version = "set_default", *skill.BundledVersion
			return changeSystemSkill(ctx, mapper, skill, input)
		}
		return true, nil
	case "set_icon":
		skill.IconKey = input.IconKey
	case "publish":
		return publishSystemSkill(ctx, mapper, skill, input.Version)
	case "set_default":
		version, err := mapper.Version(ctx, skill.UUID, input.Version)
		if err != nil {
			return false, mapNoRows(err)
		}
		if version.State != "published" {
			return false, ErrInvalidState
		}
		if skill.LatestVersion == nil {
			skill.Listed = true
		}
		skill.LatestVersion = new(version.Version)
	case "list":
		if skill.LatestVersion == nil {
			return false, ErrInvalidState
		}
		version, err := mapper.Version(ctx, skill.UUID, *skill.LatestVersion)
		if err != nil {
			return false, err
		}
		if version.State != "published" {
			return false, ErrInvalidState
		}
		skill.Listed = true
	case "unlist":
		skill.Listed = false
	default:
		return false, ErrInvalidState
	}
	return true, nil
}
