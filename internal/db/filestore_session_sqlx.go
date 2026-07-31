package db

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/jmoiron/sqlx"
)

var insertSessionFilesystemSQLXQuery = `
		insert into filestore_filesystems (
			external_id, organization_uuid, workspace_uuid, session_uuid,
			code_session_uuid, created_by_api_key_uuid, created_at, updated_at
		)
		select
			:filesystem_external_id, w.organization_uuid, w.uuid,
			:session_uuid, null, ak.uuid, :created_at, :created_at
		from workspaces w
		join api_keys ak
			on ak.uuid = :created_by_api_key_uuid
			and ak.workspace_uuid = w.uuid
		where w.uuid = :workspace_uuid
			and w.organization_uuid = :organization_uuid
			and w.archived_at is null
		on conflict on constraint filestore_filesystems_workspace_uuid_external_id_key do nothing
		returning ` + filestoreFilesystemColumns() + `
	`

const (
	sessionFilesystemExternalIDConflictQuery = `
		select exists (
			select 1
			from filestore_filesystems fs
			where fs.workspace_uuid = :workspace_uuid
				and fs.external_id = :filesystem_external_id
		)
	`
)

func insertSessionFilesystemSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
) (FilestoreFilesystem, error) {
	createdAt := filestoreNow(session.CreatedAt)
	return createFilestoreFilesystemWithGeneratedID(
		func() (string, error) {
			return ids.New(filestoreFilesystemIDPrefix)
		},
		func(externalID string) (FilestoreFilesystem, bool, error) {
			filesystem, err := getFilestoreFilesystemSQLX(
				ctx,
				tx,
				insertSessionFilesystemSQLXQuery,
				sessionFilesystemArguments(session, externalID, createdAt),
			)
			if err == nil {
				return filesystem, true, nil
			}
			if isUniqueViolationOnConstraint(err, filestoreWorkspaceSessionKey) {
				return FilestoreFilesystem{}, false, ErrDuplicate
			}
			if !errors.Is(err, ErrNotFound) {
				return FilestoreFilesystem{}, false, err
			}

			var externalIDConflict bool
			if err := namedGetContext(
				ctx,
				tx,
				&externalIDConflict,
				sessionFilesystemExternalIDConflictQuery,
				sessionFilesystemArguments(session, externalID, createdAt),
			); err != nil {
				return FilestoreFilesystem{}, false, err
			}
			if !externalIDConflict {
				return FilestoreFilesystem{}, false, ErrPreconditionFailed
			}
			return FilestoreFilesystem{}, false, nil
		},
	)
}

func sessionFilesystemArguments(session Session, externalID string, createdAt time.Time) map[string]any {
	return map[string]any{
		"filesystem_external_id":  externalID,
		"session_uuid":            dbUUID(session.UUID),
		"organization_uuid":       dbUUID(session.OrganizationUUID),
		"workspace_uuid":          dbUUID(session.WorkspaceUUID),
		"created_by_api_key_uuid": dbUUID(session.CreatedByAPIKeyUUID),
		"created_at":              createdAt,
	}
}
