package filestore

import (
	"context"
	"io"
	"path"
	"strings"
)

func (s *Service) ReadSessionSkill(ctx context.Context, workspaceUUID, sessionID, directory string) ([]byte, error) {
	if directory == "" || directory == "." || directory == ".." || strings.ContainsAny(directory, "/\\\x00") {
		return nil, errSessionSkillDirectory
	}
	filesystem, err := s.db.GetFilestoreFilesystemBySession(ctx, workspaceUUID, sessionID)
	if err != nil {
		return nil, err
	}
	principal := Principal{WorkspaceUUID: workspaceUUID, FilesystemUUID: filesystem.UUID, FilesystemExternalID: filesystem.ExternalID, Readonly: true}
	result, apiErr := s.ReadFile(ctx, principal, readFileRequest{FilesystemID: filesystem.ExternalID, Path: path.Join("/skills", directory, "SKILL.md")})
	if apiErr != nil {
		return nil, apiErr
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, maxSkillArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSkillArchiveBytes {
		return nil, errSessionSkillTooLarge
	}
	return data, nil
}
