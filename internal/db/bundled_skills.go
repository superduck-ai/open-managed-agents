package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/superduck-ai/yourbatis"
)

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
			if err := mapper.InsertVersion(ctx, version); err != nil {
				return err
			}
		} else {
			return err
		}
		_, err = mapper.Publish(ctx, stored.UUID, version.Version)
		return err
	})
}
