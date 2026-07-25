package filestore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

const filestoreCursorVersion = 1

// directoryCursor 是目录列表的键集游标。
// 除最后一项的排序键外，它还封存查询范围，防止游标被挪用于另一文件系统或另一种递归模式。
type directoryCursor struct {
	Version      int    `json:"v"`
	FilesystemID string `json:"fs"`
	Path         string `json:"path"`
	Recursive    bool   `json:"recursive"`
	LastPath     string `json:"lastPath"`
	LastID       int64  `json:"lastId"`
}

func encodeDirectoryCursor(cursor directoryCursor) (string, error) {
	cursor.Version = filestoreCursorVersion
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeDirectoryCursor(raw, filesystemID, directoryPath string, recursive bool) (directoryCursor, error) {
	if raw == "" {
		return directoryCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return directoryCursor{}, errors.New("cursor is invalid")
	}
	var cursor directoryCursor
	if err := decodeStrictJSON(data, &cursor); err != nil {
		return directoryCursor{}, errors.New("cursor is invalid")
	}
	if cursor.Version != filestoreCursorVersion || cursor.FilesystemID != filesystemID ||
		cursor.Path != directoryPath || cursor.Recursive != recursive || cursor.LastPath == "" || cursor.LastID <= 0 {
		// 游标不是独立授权凭证；将查询条件纳入校验可阻止跨目录复用，也保证翻页排序连续。
		return directoryCursor{}, errors.New("cursor does not match this listing")
	}
	return cursor, nil
}
