package filestore

import (
	"regexp"
	"strings"
)

const (
	memoryNamespaceRoot    = "/memory"
	memorySandboxMountRoot = "/mnt/memory"
)

var memorySlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type memoryFilestorePath struct {
	Slug string
	Rel  string
}

func parseMemoryFilestorePath(value string) (memoryFilestorePath, bool) {
	if value == memoryNamespaceRoot || !strings.HasPrefix(value, memoryNamespaceRoot+"/") {
		return memoryFilestorePath{}, false
	}
	rest := strings.TrimPrefix(value, memoryNamespaceRoot+"/")
	slug, remainder, found := strings.Cut(rest, "/")
	if !memorySlugPattern.MatchString(slug) {
		return memoryFilestorePath{}, false
	}
	parsed := memoryFilestorePath{Slug: slug}
	if found {
		parsed.Rel = "/" + remainder
	}
	return parsed, true
}

func classifyMemoryTransfer(source, dest string) (memoryFilestorePath, memoryFilestorePath, bool, bool) {
	src, srcOK := parseMemoryFilestorePath(source)
	dst, dstOK := parseMemoryFilestorePath(dest)
	return src, dst, srcOK && dstOK && src.Slug == dst.Slug, srcOK || dstOK
}

func memorySlugFromMountPath(mountPath string) string {
	if !strings.HasPrefix(mountPath, memorySandboxMountRoot+"/") {
		return ""
	}
	return strings.TrimPrefix(mountPath, memorySandboxMountRoot+"/")
}

func (p memoryFilestorePath) filestorePath() string {
	if p.Rel == "" {
		return memoryNamespaceRoot + "/" + p.Slug
	}
	return memoryNamespaceRoot + "/" + p.Slug + p.Rel
}
