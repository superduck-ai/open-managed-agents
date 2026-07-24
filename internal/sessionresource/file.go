// Package sessionresource defines the shared managed-agent Session resource
// contract used by the Sessions and Deployments API boundaries.
package sessionresource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

const FileType = "file"

// FileSpec is the canonical File resource configuration after API defaults and
// path validation have been applied. It intentionally excludes a Session
// resource ID because Deployment templates exist before a Session is created.
type FileSpec struct {
	fileID    string
	mountPath string
}

type filePayload struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	FileID    string `json:"file_id"`
	Source    string `json:"source"`
	MountPath string `json:"mount_path"`
}

// ParseFileID validates the file_id field before a caller resolves the Files
// API object in its own workspace and error-mapping boundary.
func ParseFileID(fields map[string]json.RawMessage) (string, error) {
	raw, ok := fields["file_id"]
	if !ok {
		return "", errors.New("file_id is required")
	}
	return requiredString(raw, "file_id")
}

// NormalizeFileSpec applies the public source and mount_path defaults after the
// caller has resolved fileID in the current workspace.
func NormalizeFileSpec(fileID string, sourceRaw, mountPathRaw json.RawMessage) (FileSpec, error) {
	if strings.TrimSpace(fileID) == "" {
		return FileSpec{}, errors.New("file_id must be non-empty")
	}
	if _, err := sandboxmount.NormalizeFileSource(sourceRaw); err != nil {
		return FileSpec{}, err
	}
	mountPath, err := optionalString(
		mountPathRaw,
		sandboxmount.DefaultFileMountPath(fileID),
		"mount_path",
	)
	if err != nil {
		return FileSpec{}, err
	}
	if err := sandboxmount.ValidateFileMountPath(mountPath); err != nil {
		return FileSpec{}, err
	}
	return FileSpec{fileID: fileID, mountPath: mountPath}, nil
}

// ParseStoredFileSpec strictly reconstructs a normalized File resource from a
// Deployment template. Stored data does not receive API defaults because it
// must already be in the canonical contract.
func ParseStoredFileSpec(raw json.RawMessage) (FileSpec, error) {
	var payload filePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return FileSpec{}, errors.New("stored file resource is invalid")
	}
	if payload.Type != FileType {
		return FileSpec{}, fmt.Errorf("stored file resource type must be %q", FileType)
	}
	if strings.TrimSpace(payload.FileID) == "" {
		return FileSpec{}, errors.New("stored file resource file_id is required")
	}
	if payload.Source != sandboxmount.FileSource {
		return FileSpec{}, fmt.Errorf("stored file resource source must be %q", sandboxmount.FileSource)
	}
	if err := sandboxmount.ValidateFileMountPath(payload.MountPath); err != nil {
		return FileSpec{}, err
	}
	return FileSpec{
		fileID:    payload.FileID,
		mountPath: payload.MountPath,
	}, nil
}

// ParseFilePayload validates a persisted Session resource payload against the
// owning session_resources row before deriving its Filestore binding.
func ParseFilePayload(raw json.RawMessage, resourceID string) (FileSpec, error) {
	var payload filePayload
	if err := json.Unmarshal(raw, &payload); err != nil ||
		strings.TrimSpace(payload.ID) == "" ||
		payload.ID != resourceID ||
		payload.Type != FileType ||
		strings.TrimSpace(payload.FileID) == "" ||
		payload.Source != sandboxmount.FileSource {
		return FileSpec{}, errors.New("file resource payload is invalid")
	}
	if err := sandboxmount.ValidateFileMountPath(payload.MountPath); err != nil {
		return FileSpec{}, err
	}
	return FileSpec{
		fileID:    payload.FileID,
		mountPath: payload.MountPath,
	}, nil
}

// PayloadFields returns the canonical JSON fields for either a Deployment
// template or a Session resource. resourceID is empty only for Deployment
// storage.
func (s FileSpec) PayloadFields(resourceID string) map[string]any {
	fields := map[string]any{
		"type":       FileType,
		"file_id":    s.fileID,
		"source":     sandboxmount.FileSource,
		"mount_path": s.mountPath,
	}
	if resourceID != "" {
		fields["id"] = resourceID
	}
	return fields
}

// SessionFileMount maps the public mount_path into the authoritative uploads
// namespace used by the Session resource write transaction.
func (s FileSpec) SessionFileMount(resourceID string) (db.SessionFileMount, error) {
	backingPath, err := sandboxmount.FileBackingPath(s.mountPath)
	if err != nil {
		return db.SessionFileMount{}, err
	}
	return db.SessionFileMount{
		ResourceExternalID: resourceID,
		FileExternalID:     s.fileID,
		Path:               backingPath,
	}, nil
}

// ValidateFileSpecs applies the Session/Deployment aggregate file-count and
// mount-path conflict contract to normalized specs.
func ValidateFileSpecs(specs []FileSpec) error {
	mountPaths := make([]string, 0, len(specs))
	for _, spec := range specs {
		mountPaths = append(mountPaths, spec.mountPath)
	}
	return sandboxmount.ValidateFileMountPaths(mountPaths)
}

func requiredString(raw json.RawMessage, name string) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", fmt.Errorf("%s is required", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

func optionalString(raw json.RawMessage, fallback, name string) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return fallback, nil
	}
	return requiredString(raw, name)
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
