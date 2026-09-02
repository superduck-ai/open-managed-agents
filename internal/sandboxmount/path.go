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

// ValidateFileMountPath 校验 File resource 在固定 uploads 根目录下的相对命名空间路径。
// 对外合同使用绝对路径形式；Sandbox 中的最终路径始终加上 SandboxUploadsMount 前缀。
func ValidateFileMountPath(mountPath string) error {
	_, err := FileBackingPath(mountPath)
	return err
}

// FileBackingPath 将对外 mount_path 映射到 Session Filestore 的固定 uploads namespace。
func FileBackingPath(mountPath string) (string, error) {
	if err := filestorepath.Validate(mountPath, false); err != nil {
		return "", fmt.Errorf("mount_path %w", err)
	}
	for _, value := range mountPath {
		if unicode.IsControl(value) {
			return "", errors.New("mount_path must not contain control characters")
		}
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

// ValidateFileMountPaths 校验重复路径与祖先/后代冲突。
func ValidateFileMountPaths(mountPaths []string) error {
	backingPaths := make([]string, len(mountPaths))
	for index, mountPath := range mountPaths {
		backingPath, err := FileBackingPath(mountPath)
		if err != nil {
			return err
		}
		backingPaths[index] = backingPath
	}
	for index, current := range mountPaths {
		currentBackingPath := backingPaths[index]
		for otherIndex, other := range mountPaths[index+1:] {
			otherBackingPath := backingPaths[index+1+otherIndex]
			if currentBackingPath == otherBackingPath {
				return fmt.Errorf("resource mount_path is duplicated: %s", current)
			}
			if filestorepath.IsDescendant(currentBackingPath, otherBackingPath) ||
				filestorepath.IsDescendant(otherBackingPath, currentBackingPath) {
				return fmt.Errorf("resource mount_path values conflict by ancestry: %s and %s", current, other)
			}
		}
	}
	return nil
}
