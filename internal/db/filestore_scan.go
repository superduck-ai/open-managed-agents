package db

import (
	"bytes"
	"encoding/json"
)

func virtualFilestoreRoot(filesystem FilestoreFilesystem) SessionResourceFile {
	// 根目录与文件系统同生共灭，虚拟投影可省去一条永远存在且不可删除的特殊 entries 记录。
	return SessionResourceFile{
		UUID:                  filesystem.UUID,
		ExternalID:            filesystem.ExternalID,
		OrganizationUUID:      filesystem.OrganizationUUID,
		WorkspaceUUID:         filesystem.WorkspaceUUID,
		SessionUUID:           filesystem.SessionUUID,
		Kind:                  SessionResourceFileKindDirectory,
		Path:                  "/",
		Metadata:              json.RawMessage(`{}`),
		AuthorizationMetadata: json.RawMessage(`{}`),
		Tags:                  []string{},
		CreatedAt:             filesystem.CreatedAt,
		UpdatedAt:             filesystem.UpdatedAt,
	}
}

func filestoreJSONObject(value json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte(`{}`)
	}
	return trimmed
}

func filestoreTags(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}

func filestoreNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func filestoreString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func filestoreInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
