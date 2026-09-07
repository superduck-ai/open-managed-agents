// Package sessionresource defines the shared managed-agent Session resource
// contract used by the Sessions and Deployments API boundaries.
package sessionresource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const (
	FileType         = sessioncontract.FileResourceType
	MaxFileResources = sessioncontract.MaxFileResources
)

// FileSpec is the canonical File resource configuration after API defaults and
// path validation have been applied. It intentionally excludes a Session
// resource ID because Deployment templates exist before a Session is created.
type FileSpec struct {
	fileID    string
	mountPath string
}

// SessionFileBinding is the persistence-neutral mapping from one Session
// resource to the Files API object and backing path it borrows.
type SessionFileBinding struct {
	ResourceID string
	FileID     string
	Path       string
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
func ParseFileID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("file_id is required")
	}
	return requiredString(raw, "file_id")
}

// NormalizeFileSpec applies the public source and mount_path defaults after the
// caller has resolved fileID in the current workspace.
func NormalizeFileSpec(fileID, filename string, sourceRaw, mountPathRaw json.RawMessage) (FileSpec, error) {
	if fileID == "" {
		return FileSpec{}, errors.New("file_id must be non-empty")
	}
	if _, err := sandboxmount.NormalizeFileSource(sourceRaw); err != nil {
		return FileSpec{}, err
	}
	mountPath, err := optionalString(
		mountPathRaw,
		sandboxmount.DefaultFileMountPath(fileID, filename),
		"mount_path",
	)
	if err != nil {
		return FileSpec{}, err
	}
	return newFileSpec(fileID, mountPath)
}

// RestoreFileSpec 从已经解码的规范 Deployment 或 Session resource 字段恢复 FileSpec。
func RestoreFileSpec(fileID, source, mountPath string) (FileSpec, error) {
	if fileID == "" {
		return FileSpec{}, errors.New("stored file resource file_id is required")
	}
	if source != sandboxmount.FileSource {
		return FileSpec{}, fmt.Errorf("stored file resource source must be %q", sandboxmount.FileSource)
	}
	return newFileSpec(fileID, mountPath)
}

// ParseFilePayload validates a persisted Session resource payload against the
// owning session_resources row before deriving its Filestore binding.
func ParseFilePayload(raw json.RawMessage, resourceID string) (FileSpec, error) {
	var payload filePayload
	if err := json.Unmarshal(raw, &payload); err != nil ||
		payload.ID == "" ||
		payload.ID != resourceID ||
		payload.Type != FileType {
		return FileSpec{}, errors.New("file resource payload is invalid")
	}
	spec, err := RestoreFileSpec(payload.FileID, payload.Source, payload.MountPath)
	if err != nil {
		return FileSpec{}, fmt.Errorf("file resource payload is invalid: %w", err)
	}
	return spec, nil
}

func newFileSpec(fileID, mountPath string) (FileSpec, error) {
	if _, err := sandboxmount.FileBackingPath(mountPath); err != nil {
		return FileSpec{}, err
	}
	return FileSpec{fileID: fileID, mountPath: mountPath}, nil
}

// FileID 返回此配置保存的原始 Files API resource ID。
func (s FileSpec) FileID() string { return s.fileID }

// MountPath 返回此配置保存的公开 mount_path。
func (s FileSpec) MountPath() string { return s.mountPath }

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

// SessionFileBinding maps the public mount_path into the authoritative uploads
// namespace used by the Session resource write transaction.
func (s FileSpec) SessionFileBinding(resourceID string) (SessionFileBinding, error) {
	backingPath, err := sandboxmount.FileBackingPath(s.mountPath)
	if err != nil {
		return SessionFileBinding{}, err
	}
	return SessionFileBinding{
		ResourceID: resourceID,
		FileID:     s.fileID,
		Path:       backingPath,
	}, nil
}

// ValidateFileSpecs applies the Session/Deployment aggregate file-count and
// mount-path conflict contract to normalized specs.
func ValidateFileSpecs(specs []FileSpec) error {
	if len(specs) > MaxFileResources {
		return fmt.Errorf("at most %d managed-agent file resources are allowed", MaxFileResources)
	}
	backingPaths := make([]string, len(specs))
	for index, spec := range specs {
		backingPath, err := sandboxmount.FileBackingPath(spec.mountPath)
		if err != nil {
			return err
		}
		backingPaths[index] = backingPath
	}
	for index, current := range specs {
		currentBackingPath := backingPaths[index]
		for otherOffset, other := range specs[index+1:] {
			otherBackingPath := backingPaths[index+1+otherOffset]
			if currentBackingPath == otherBackingPath {
				return fmt.Errorf("resource mount_path is duplicated: %s", current.mountPath)
			}
			if filestorepath.IsDescendant(currentBackingPath, otherBackingPath) ||
				filestorepath.IsDescendant(otherBackingPath, currentBackingPath) {
				return fmt.Errorf(
					"resource mount_path values conflict by ancestry: %s and %s",
					current.mountPath,
					other.mountPath,
				)
			}
		}
	}
	return nil
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
