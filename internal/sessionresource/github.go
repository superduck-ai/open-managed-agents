package sessionresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
)

const GitHubRepositoryType = "github_repository"

var githubRepositoryPath = regexp.MustCompile(`^/[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)
var gitCommitSHA = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)

type GitHubCheckout struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	SHA  string `json:"sha,omitempty"`
}

type GitHubSpec struct {
	URL       string          `json:"url"`
	MountPath string          `json:"mount_path"`
	Checkout  *GitHubCheckout `json:"checkout,omitempty"`
}

// NormalizeGitHubSpec is shared by Session and Deployment creation. Only an
// explicit GitHub HTTPS repository identity is accepted, never URL credentials.
func NormalizeGitHubSpec(urlRaw, mountPathRaw, checkoutRaw json.RawMessage) (GitHubSpec, error) {
	repoURL, err := requiredString(urlRaw, "url")
	if err != nil {
		return GitHubSpec{}, err
	}
	if err := ValidateGitHubURL(repoURL); err != nil {
		return GitHubSpec{}, err
	}
	mountPath, err := optionalString(mountPathRaw, DefaultGitHubRepositoryMountPath(repoURL), "mount_path")
	if err != nil {
		return GitHubSpec{}, err
	}
	if err := ValidateGitHubMountPaths([]string{mountPath}); err != nil {
		return GitHubSpec{}, err
	}
	checkout, err := parseGitHubCheckout(checkoutRaw)
	if err != nil {
		return GitHubSpec{}, err
	}
	return GitHubSpec{URL: repoURL, MountPath: mountPath, Checkout: checkout}, nil
}

func ValidateGitHubURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(rawURL, "#") ||
		parsed.RawPath != "" || !githubRepositoryPath.MatchString(parsed.Path) || strings.HasSuffix(parsed.Path, ".git") {
		return errors.New("url must be https://github.com/owner/repo without credentials, query, fragment, or .git suffix")
	}
	name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	if name == "." || name == ".." {
		return errors.New("url repository name is invalid")
	}
	return nil
}

// DefaultGitHubRepositoryMountPath follows the public /workspace/<repo> default.
func DefaultGitHubRepositoryMountPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "/workspace/repository"
	}
	name := strings.TrimSuffix(parsed.Path[strings.LastIndex(parsed.Path, "/")+1:], ".git")
	if name == "" {
		name = "repository"
	}
	return "/workspace/" + name
}

func parseGitHubCheckout(raw json.RawMessage) (*GitHubCheckout, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var checkout GitHubCheckout
	if err := json.Unmarshal(raw, &checkout); err != nil {
		return nil, errors.New("checkout must be an object")
	}
	if err := validateGitHubCheckout(&checkout); err != nil {
		return nil, err
	}
	return &checkout, nil
}

func validateGitHubCheckout(checkout *GitHubCheckout) error {
	switch checkout.Type {
	case "branch":
		if checkout.SHA != "" || !validGitBranch(checkout.Name) {
			return errors.New("checkout.name must be a valid Git branch name and checkout.sha must be omitted")
		}
	case "commit":
		if checkout.Name != "" || !gitCommitSHA.MatchString(checkout.SHA) {
			return errors.New("checkout.sha must be a 7–64 character hexadecimal commit SHA and checkout.name must be omitted")
		}
		checkout.SHA = strings.ToLower(checkout.SHA)
	default:
		return errors.New("checkout.type must be branch or commit")
	}
	return nil
}

func validGitBranch(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 255 || name == "@" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") ||
		strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.ContainsAny(name, " ~^:?*[\\") || strings.ContainsFunc(name, unicode.IsControl) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// ValidateGitHubMountPaths rejects reserved runtime directories and ambiguous or
// overlapping working trees. File mounts have a separate /mnt/session namespace.
func ValidateGitHubMountPaths(paths []string) error {
	for i, current := range paths {
		if err := filestorepath.Validate(current, false); err != nil {
			return fmt.Errorf("mount_path %w", err)
		}
		if !strings.HasPrefix(current, "/workspace/") || strings.Contains(current, "\\") || strings.ContainsFunc(current, unicode.IsControl) {
			return errors.New("GitHub mount_path must be a directory below /workspace without control characters or backslashes")
		}
		for _, part := range strings.Split(strings.TrimPrefix(current, "/workspace/"), "/") {
			if part == ".git" || part == ".claude" || part == ".oma" {
				return errors.New("GitHub mount_path must not use .git, .claude, or .oma directories")
			}
		}
		for _, other := range paths[:i] {
			if current == other || filestorepath.IsDescendant(current, other) || filestorepath.IsDescendant(other, current) {
				return errors.New("GitHub resource mount_path values must not overlap")
			}
		}
	}
	return nil
}

// ParseStoredGitHubSpec validates persisted configuration before runtime use.
// Unlike API normalization, missing URL/path are errors, never silent defaults.
func ParseStoredGitHubSpec(raw json.RawMessage) (GitHubSpec, error) {
	var spec GitHubSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return GitHubSpec{}, errors.New("stored GitHub resource is invalid")
	}
	if err := ValidateGitHubURL(spec.URL); err != nil {
		return GitHubSpec{}, err
	}
	if err := ValidateGitHubMountPaths([]string{spec.MountPath}); err != nil {
		return GitHubSpec{}, err
	}
	if spec.Checkout != nil {
		if err := validateGitHubCheckout(spec.Checkout); err != nil {
			return GitHubSpec{}, err
		}
	}
	return spec, nil
}

// ValidateGitHubSpecs rejects ambiguous credential ownership as well as working
// tree overlaps. GitHub repository identities are case-insensitive.
func ValidateGitHubSpecs(specs []GitHubSpec) error {
	paths := make([]string, 0, len(specs))
	repositories := make(map[string]bool, len(specs))
	for _, spec := range specs {
		key := strings.ToLower(spec.URL)
		if repositories[key] {
			return errors.New("GitHub repository URLs must not be duplicated within resources")
		}
		repositories[key] = true
		paths = append(paths, spec.MountPath)
	}
	return ValidateGitHubMountPaths(paths)
}
