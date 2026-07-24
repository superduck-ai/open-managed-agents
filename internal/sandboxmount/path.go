// Package sandboxmount 定义 managed-agent File resource 在 Sandbox 与 Filestore
// 之间共享的挂载路径合同。
package sandboxmount

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
)

const (
	// FileSource 是 managed-agent File resource 唯一允许的 Filestore source。
	FileSource = "/uploads"
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
func DefaultFileMountPath(fileID string) string {
	return "/" + fileID
}

// ValidateFileMountPath 校验 File resource 在固定 uploads 根目录下的相对命名空间路径。
// 对外合同使用绝对路径形式；Sandbox 中的最终路径始终加上 /mnt/session/uploads 前缀。
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
	if err := filestorepath.Validate(backingPath, false); err != nil {
		return "", fmt.Errorf("source + mount_path %w", err)
	}
	return backingPath, nil
}

// ValidateFileMountPaths 校验重复路径与祖先/后代冲突。
func ValidateFileMountPaths(mountPaths []string) error {
	for index, current := range mountPaths {
		if err := ValidateFileMountPath(current); err != nil {
			return err
		}
		for _, other := range mountPaths[index+1:] {
			if current == other {
				return fmt.Errorf("resource mount_path is duplicated: %s", current)
			}
			if filestorepath.IsDescendant(current, other) || filestorepath.IsDescendant(other, current) {
				return fmt.Errorf("resource mount_path values conflict by ancestry: %s and %s", current, other)
			}
		}
	}
	return nil
}
