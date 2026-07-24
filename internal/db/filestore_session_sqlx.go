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
			:filesystem_external_id, o.uuid, w.uuid, :session_uuid,
			null, ak.uuid, :created_at, :created_at
		from organizations o
		join workspaces w
			on w.id = :workspace_id
			and w.organization_id = o.id
		join api_keys ak
			on ak.id = :created_by_api_key_id
			and ak.workspace_id = w.id
		where o.id = :organization_id
		on conflict on constraint filestore_filesystems_workspace_uuid_external_id_key do nothing
		returning ` + filestoreFilesystemColumns() + `
	`

const (
	sessionFilesystemExternalIDConflictQuery = `
		select exists (
			select 1
			from filestore_filesystems fs
			join workspaces w on w.uuid = fs.workspace_uuid
			where w.id = :workspace_id
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
		"filesystem_external_id": externalID,
		"session_uuid":           session.UUID,
		"organization_id":        session.OrganizationID,
		"workspace_id":           session.WorkspaceID,
		"created_by_api_key_id":  session.CreatedByAPIKeyID,
		"created_at":             createdAt,
	}
}
