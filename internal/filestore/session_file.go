package filestore

import (
	"bytes"
	"context"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) WriteSessionFile(ctx context.Context, workspaceUUID, sessionExternalID, directory, path, mediaType string, body []byte) error {
	if len(body) == 0 {
		return invalidArgument("file body is required")
	}
	filesystem, principal, err := s.sessionFileScope(ctx, workspaceUUID, sessionExternalID)
	if err != nil {
		return err
	}
	if directory != "" {
		if _, apiErr := s.MakeDirectory(ctx, principal, makeDirectoryRequest{
			FilesystemID: filesystem.ExternalID,
			Path:         directory,
			MakeParents:  true,
		}); apiErr != nil {
			return apiErr
		}
	}
	_, apiErr := s.CreateFile(ctx, principal, createFileParams{
		FilesystemID: filesystem.ExternalID,
		Path:         path,
		MediaType:    mediaType,
	}, bytes.NewReader(body))
	if apiErr != nil {
		return apiErr
	}
	return nil
}

func (s *Service) ReadSessionFile(ctx context.Context, workspaceUUID, sessionExternalID, path string, limit int64) ([]byte, error) {
	filesystem, principal, err := s.sessionFileScope(ctx, workspaceUUID, sessionExternalID)
	if err != nil {
		return nil, err
	}
	result, apiErr := s.ReadFile(ctx, principal, readFileRequest{
		FilesystemID: filesystem.ExternalID,
		Path:         path,
	})
	if apiErr != nil {
		if IsNotFound(apiErr) {
			return nil, db.ErrNotFound
		}
		return nil, apiErr
	}
	defer result.Body.Close()
	if limit <= 0 {
		limit = 1 << 20
	}
	payload, err := io.ReadAll(io.LimitReader(result.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, errSessionFileTooLarge
	}
	return payload, nil
}

func (s *Service) sessionFileScope(ctx context.Context, workspaceUUID, sessionExternalID string) (db.FilestoreFilesystem, Principal, error) {
	filesystem, err := s.db.GetFilestoreFilesystemBySession(ctx, workspaceUUID, sessionExternalID)
	if err != nil {
		return db.FilestoreFilesystem{}, Principal{}, err
	}
	return filesystem, Principal{
		OrganizationUUID:     filesystem.OrganizationUUID,
		WorkspaceUUID:        filesystem.WorkspaceUUID,
		FilesystemUUID:       filesystem.UUID,
		FilesystemExternalID: filesystem.ExternalID,
	}, nil
}
