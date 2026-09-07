// Package sandboxmount 定义 managed-agent File resource 在 Sandbox 与 Filestore
// 之间共享的挂载路径合同。
package sandboxmount

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
)

const (
	// FileSource 是 managed-agent File resource 唯一允许的 Filestore source。
	FileSource = "/uploads"
	// SandboxUploadsMount 是 FileSource 在 Sandbox 中的挂载点。
	SandboxUploadsMount = "/mnt/session/uploads"
	// OutputsRoot 是 session 输出文件的 Filestore 命名空间。
	OutputsRoot = "/outputs"
)

// NormalizeFileSource 为省略的 source 补默认值，并拒绝 null 或其他 namespace。
func NormalizeFileSource(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return FileSource, nil
	}
	var source string
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", errors.New("source must be a string")
	}
	if source != FileSource {
		return "", fmt.Errorf("source must be %q", FileSource)
	}
	return source, nil
}

// DefaultFileMountPath 返回 File resource 在 uploads namespace 中的默认路径。
func DefaultFileMountPath(fileID, filename string) string {
	if filename == "" {
		filename = fileID
	}
	return FileSource + "/" + filename
}

// FileBackingPath 将对外 mount_path 映射到 Session Filestore 的固定 uploads namespace。
func FileBackingPath(mountPath string) (string, error) {
	if err := validateBackingPath("mount_path", mountPath); err != nil {
		return "", err
	}
	backingPath := FileSource + mountPath
	if strings.HasPrefix(mountPath, FileSource+"/") {
		backingPath = mountPath
	}
	if err := filestorepath.Validate(backingPath, false); err != nil {
		return "", fmt.Errorf("source + mount_path %w", err)
	}
	return backingPath, nil
}

// SandboxFilePath maps an authoritative /uploads Filestore path to the path
// visible inside the managed-agent Sandbox.
func SandboxFilePath(backingPath string) (string, error) {
	if err := validateBackingPath("file backing path", backingPath); err != nil {
		return "", err
	}
	relativePath, ok := strings.CutPrefix(backingPath, FileSource+"/")
	if !ok {
		return "", fmt.Errorf("file backing path must be under %q", FileSource)
	}
	return SandboxUploadsMount + "/" + relativePath, nil
}

func validateBackingPath(label, value string) error {
	if err := filestorepath.Validate(value, false); err != nil {
		return fmt.Errorf("%s %w", label, err)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain control characters", label)
		}
	}
	return nil
}
