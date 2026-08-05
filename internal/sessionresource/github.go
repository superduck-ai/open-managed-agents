package sessionresource

import (
	"net/url"
	"strings"
)

// DefaultGitHubRepositoryMountPath 根据仓库 URL 生成 Anthropic 兼容的默认容器路径。
func DefaultGitHubRepositoryMountPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "/workspace/repository"
	}
	name := strings.TrimSuffix(strings.Trim(strings.TrimSpace(parsed.Path), "/"), ".git")
	if index := strings.LastIndex(name, "/"); index >= 0 {
		name = name[index+1:]
	}
	if name == "" {
		name = "repository"
	}
	return "/workspace/" + name
}
