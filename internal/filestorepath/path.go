// Package filestorepath 定义 Filestore 的规范路径语义。
// HTTP 边界与数据库写入共用本包，避免两层对同一路径作出不同解释。
package filestorepath

import (
	"errors"
	"path"
	"strings"
	"unicode/utf8"
)

// MaxBytes 是路径按 UTF-8 编码后的最大字节数，与数据库约束保持一致。
const MaxBytes = 4096

// Validate 校验绝对路径的规范形式。allowRoot 决定当前操作是否接受根目录“/”。
// 本函数只验形，不擅自清洗输入，以免多个外部路径意外归并为同一资源。
func Validate(value string, allowRoot bool) error {
	if value == "" || !strings.HasPrefix(value, "/") {
		return errors.New("path must be absolute")
	}
	if !utf8.ValidString(value) || len(value) > MaxBytes {
		return errors.New("path is invalid or too long")
	}
	if strings.ContainsRune(value, '\x00') {
		return errors.New("path must not contain NUL")
	}
	if value == "/" {
		if allowRoot {
			return nil
		}
		return errors.New("root path is not allowed for this operation")
	}
	if strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return errors.New("path must not contain empty segments")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if segment == "." || segment == ".." {
			return errors.New("path must not contain dot segments")
		}
	}
	return nil
}

// Parent 返回规范路径的父目录；根目录的父目录仍为根目录。
func Parent(value string) string {
	if value == "/" {
		return "/"
	}
	parent := path.Dir(value)
	if parent == "." {
		return "/"
	}
	return parent
}

// IsDescendant 判断 candidate 是否为 ancestor 的严格后代，祖先本身不算后代。
func IsDescendant(candidate, ancestor string) bool {
	if ancestor == "/" {
		return candidate != "/" && strings.HasPrefix(candidate, "/")
	}
	return strings.HasPrefix(candidate, ancestor+"/")
}

// DirectoryChain 将路径展开为从浅到深的目录链，供 makeParents 逐级创建。
func DirectoryChain(value string) []string {
	if value == "/" {
		return nil
	}
	segments := strings.Split(strings.TrimPrefix(value, "/"), "/")
	result := make([]string, 0, len(segments))
	current := ""
	for _, segment := range segments {
		current += "/" + segment
		result = append(result, current)
	}
	return result
}
