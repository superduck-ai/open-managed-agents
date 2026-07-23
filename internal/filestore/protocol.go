package filestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// protoInt64 使 Filestore 线协议与 ProtoJSON 保持一致：64 位整数编码为 JSON 字符串，
// 解码时则兼容字符串与数字两种表示，便于不同语言的客户端安全互通。
type protoInt64 int64

func (v protoInt64) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatInt(int64(v), 10))
}

func (v *protoInt64) UnmarshalJSON(data []byte) error {
	if v == nil {
		return fmt.Errorf("decode int64 into nil destination")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("empty int64 value")
	}
	value := string(data)
	if data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid int64 value %q", value)
	}
	*v = protoInt64(parsed)
	return nil
}

type listDirectoryRequest struct {
	FilesystemID string     `json:"filesystemId"`
	Path         string     `json:"path"`
	Limit        protoInt64 `json:"limit,omitempty"`
	Cursor       string     `json:"cursor,omitempty"`
	Recursive    bool       `json:"recursive,omitempty"`
}

type makeDirectoryRequest struct {
	FilesystemID string `json:"filesystemId"`
	Path         string `json:"path"`
	MakeParents  bool   `json:"makeParents,omitempty"`
}

type removeDirectoryRequest struct {
	FilesystemID string `json:"filesystemId"`
	Path         string `json:"path"`
	Recursive    bool   `json:"recursive,omitempty"`
}

type createFileParams struct {
	FilesystemID      string                 `json:"filesystemId"`
	Path              string                 `json:"path"`
	Metadata          map[string]any         `json:"metadata,omitempty"`
	MediaType         string                 `json:"mediaType"`
	Authorization     *authorizationMetadata `json:"authorizationMetadata,omitempty"`
	Tags              []string               `json:"tags,omitempty"`
	OverwriteExisting bool                   `json:"overwriteExisting,omitempty"`
	TTLSeconds        protoInt64             `json:"ttlSeconds,omitempty"`
}

type authorizationMetadata struct {
	Intent       string `json:"intent,omitempty"`
	Downloadable bool   `json:"downloadable,omitempty"`
}

type copyMoveFileRequest struct {
	FilesystemID      string `json:"filesystemId"`
	Source            string `json:"source"`
	Destination       string `json:"destination"`
	OverwriteExisting bool   `json:"overwriteExisting,omitempty"`
}

type moveDirectoryRequest struct {
	FilesystemID string `json:"filesystemId"`
	Source       string `json:"source"`
	Destination  string `json:"destination"`
}

type readFileRequest struct {
	FilesystemID string         `json:"filesystemId"`
	Path         string         `json:"path"`
	Range        *readFileRange `json:"range,omitempty"`
}

type readFileRange struct {
	Offset protoInt64 `json:"offset,omitempty"`
	Length protoInt64 `json:"length,omitempty"`
}

type pathRequest struct {
	FilesystemID string `json:"filesystemId"`
	Path         string `json:"path"`
}

type filePayload struct {
	UUID              string         `json:"uuid,omitempty"`
	CreatedAt         string         `json:"createdAt,omitempty"`
	Size              protoInt64     `json:"size,omitempty"`
	MediaType         string         `json:"mediaType,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	MD5               string         `json:"md5,omitempty"`
	WorkspaceTaggedID string         `json:"workspaceTaggedId,omitempty"`
	DetectedMimeType  string         `json:"detectedMimeType,omitempty"`
	Downloadable      bool           `json:"downloadable,omitempty"`
	Tags              []string       `json:"tags,omitempty"`
	FilesystemID      string         `json:"filesystemId,omitempty"`
	ExpiresAt         string         `json:"expiresAt,omitempty"`
}

type filesystemFilePayload struct {
	File         filePayload `json:"file"`
	FilesystemID string      `json:"filesystemId"`
	Path         string      `json:"path"`
}

type directoryPayload struct {
	FilesystemID string `json:"filesystemId"`
	Path         string `json:"path"`
	CreatedAt    string `json:"createdAt"`
}

type entryPayload struct {
	File      *filesystemFilePayload `json:"file,omitempty"`
	Directory *directoryPayload      `json:"directory,omitempty"`
}

type listDirectoryResponse struct {
	Entries []entryPayload `json:"entries,omitempty"`
	Cursor  string         `json:"cursor,omitempty"`
}

type fileResponse struct {
	File filesystemFilePayload `json:"file"`
}

type directoryResponse struct {
	Directory directoryPayload `json:"directory"`
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	// 未知字段通常意味着客户端版本或拼写有误；拒绝静默丢弃，避免请求“成功但未生效”。
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("request contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func normalizeMediaType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
