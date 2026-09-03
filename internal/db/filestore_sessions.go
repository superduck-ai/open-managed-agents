package db

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/superduck-ai/yourbatis"
)

func insertSessionFilesystemTx(
	ctx context.Context,
	executor yourbatis.Executor,
	session Session,
) (FilestoreFilesystem, error) {
	createdAt := filestoreNow(session.CreatedAt)
	mapper := NewFilestoreFilesystemMapper(executor)
	return createFilestoreFilesystemWithGeneratedID(
		func() (string, error) {
			return ids.New(filestoreFilesystemIDPrefix)
		},
		func(externalID string) (FilestoreFilesystem, bool, error) {
			params := sessionFilesystemInsertParameters(session, externalID, createdAt)
			row, found, err := mapper.InsertSessionFilesystem(
				ctx,
				params,
			)
			if isUniqueViolationOnConstraint(err, filestoreWorkspaceSessionKey) {
				return FilestoreFilesystem{}, false, ErrDuplicate
			}
			if err != nil {
				return FilestoreFilesystem{}, false, err
			}
			if found {
				filesystem, convertErr := row.filesystem()
				return filesystem, true, convertErr
			}

			externalIDConflict, err := mapper.SessionFilesystemExternalIDExists(
				ctx,
				session.WorkspaceUUID,
				externalID,
			)
			if err != nil {
				return FilestoreFilesystem{}, false, err
			}
			if !externalIDConflict {
				return FilestoreFilesystem{}, false, ErrPreconditionFailed
			}
			return FilestoreFilesystem{}, false, nil
		},
	)
}

func sessionFilesystemInsertParameters(session Session, externalID string, createdAt time.Time) sessionFilesystemInsertParams {
	return sessionFilesystemInsertParams{
		FilesystemExternalID: externalID,
		SessionUUID:          session.UUID,
		OrganizationUUID:     session.OrganizationUUID,
		WorkspaceUUID:        session.WorkspaceUUID,
		CreatedByAPIKeyUUID:  nullableString(session.CreatedByAPIKeyUUID),
		CreatedAt:            createdAt,
	}
}
