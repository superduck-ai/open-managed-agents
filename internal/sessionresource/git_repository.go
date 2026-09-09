package sessionresource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// GitRepositoryType retains the legacy API name for all HTTPS Git repositories.
const GitRepositoryType = "github_repository"

var ErrGitTokenCrypto = errors.New("git token cryptographic operation failed")

var gitCommitSHA = regexp.MustCompile(`^([a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)

type GitRepositoryCheckout struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	SHA  string `json:"sha,omitempty"`
}

type GitRepositorySpec struct {
	URL       string                 `json:"url"`
	MountPath string                 `json:"mount_path"`
	Checkout  *GitRepositoryCheckout `json:"checkout,omitempty"`
}

func NormalizeGitRepositorySpec(urlRaw, mountPathRaw, checkoutRaw json.RawMessage) (GitRepositorySpec, error) {
	repoURL, err := requiredString(urlRaw, "url")
	if err != nil {
		return GitRepositorySpec{}, err
	}
	if err := ValidateGitRepositoryURL(repoURL); err != nil {
		return GitRepositorySpec{}, err
	}
	mountPath, err := optionalString(mountPathRaw, defaultGitRepositoryMountPath(repoURL), "mount_path")
	if err != nil {
		return GitRepositorySpec{}, err
	}
	if err := validateGitRepositoryMountPath(mountPath); err != nil {
		return GitRepositorySpec{}, err
	}
	checkout, err := parseGitRepositoryCheckout(checkoutRaw)
	if err != nil {
		return GitRepositorySpec{}, err
	}
	return GitRepositorySpec{URL: repoURL, MountPath: mountPath, Checkout: checkout}, nil
}

func ValidateGitRepositoryURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(rawURL, "#") {
		return errors.New("url must be an HTTPS repository URL without credentials, query, or fragment")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return errors.New("git repository URL must use HTTPS port 443")
	}
	repositoryPath := strings.TrimRight(parsed.Path, "/")
	if repositoryPath == "" || path.Clean(repositoryPath) != repositoryPath || parsed.RawPath != "" || strings.Contains(repositoryPath, "\\") {
		return errors.New("url must contain an unambiguous repository path")
	}
	return nil
}

// GitRepositoryKey normalizes the host and port but preserves path case and .git.
func GitRepositoryKey(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(strings.ToLower(parsed.Hostname()), port) +
		strings.TrimRight(parsed.EscapedPath(), "/"), nil
}

func defaultGitRepositoryMountPath(repoURL string) string {
	repoURL = strings.TrimRight(repoURL, "/")
	return "/workspace/" + strings.TrimSuffix(repoURL[strings.LastIndex(repoURL, "/")+1:], ".git")
}

func parseGitRepositoryCheckout(raw json.RawMessage) (*GitRepositoryCheckout, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var checkout GitRepositoryCheckout
	if err := json.Unmarshal(raw, &checkout); err != nil {
		return nil, errors.New("checkout must be an object")
	}
	if err := validateGitRepositoryCheckout(&checkout); err != nil {
		return nil, err
	}
	return &checkout, nil
}

func validateGitRepositoryCheckout(checkout *GitRepositoryCheckout) error {
	switch checkout.Type {
	case "branch":
		if checkout.SHA != "" || !validGitBranch(checkout.Name) {
			return errors.New("checkout.name must be a valid Git branch name and checkout.sha must be omitted")
		}
	case "commit":
		if checkout.Name != "" || !gitCommitSHA.MatchString(checkout.SHA) {
			return errors.New("checkout.sha must be a full 40- or 64-character hexadecimal commit SHA and checkout.name must be omitted")
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

func validateGitRepositoryMountPath(current string) error {
	if err := filestorepath.Validate(current, false); err != nil {
		return fmt.Errorf("mount_path %w", err)
	}
	if !strings.HasPrefix(current, "/workspace/") || strings.Contains(current, "\\") || strings.ContainsFunc(current, unicode.IsControl) {
		return errors.New("git mount_path must be a directory below /workspace without control characters or backslashes")
	}
	for _, part := range strings.Split(strings.TrimPrefix(current, "/workspace/"), "/") {
		if part == ".git" || part == ".claude" || part == ".oma" {
			return errors.New("git mount_path must not use .git, .claude, or .oma directories")
		}
	}
	return nil
}

// ParseStoredGitRepositorySpec rejects missing persisted fields instead of applying defaults.
func ParseStoredGitRepositorySpec(raw json.RawMessage) (GitRepositorySpec, error) {
	var spec GitRepositorySpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return GitRepositorySpec{}, errors.New("stored Git resource is invalid")
	}
	if err := ValidateGitRepositoryURL(spec.URL); err != nil {
		return GitRepositorySpec{}, err
	}
	if err := validateGitRepositoryMountPath(spec.MountPath); err != nil {
		return GitRepositorySpec{}, err
	}
	if spec.Checkout != nil {
		if err := validateGitRepositoryCheckout(spec.Checkout); err != nil {
			return GitRepositorySpec{}, err
		}
	}
	return spec, nil
}

// ValidateGitRepositoryConflicts requires individually validated specs.
func ValidateGitRepositoryConflicts(specs []GitRepositorySpec) error {
	repositories := make(map[string]bool, len(specs))
	for i, spec := range specs {
		key, err := GitRepositoryKey(spec.URL)
		if err != nil {
			return err
		}
		if repositories[key] {
			return errors.New("git repository URLs must not be duplicated within resources")
		}
		repositories[key] = true
		for _, other := range specs[:i] {
			if spec.MountPath == other.MountPath || filestorepath.IsDescendant(spec.MountPath, other.MountPath) || filestorepath.IsDescendant(other.MountPath, spec.MountPath) {
				return errors.New("git resource mount_path values must not overlap")
			}
		}
	}
	return nil
}

type storedGitToken struct {
	Envelope secrets.Envelope `json:"envelope"`
}

func ParseGitTokenInput(tokenJSON json.RawMessage) (string, error) {
	var token string
	if len(tokenJSON) != 0 {
		if err := json.Unmarshal(tokenJSON, &token); err != nil {
			return "", errors.New("authorization_token must be a string or null")
		}
	}
	if len(token) > 8192 || strings.ContainsFunc(token, unicode.IsSpace) || strings.ContainsFunc(token, unicode.IsControl) {
		return "", errors.New("authorization_token must not contain whitespace or control characters and must be at most 8192 bytes")
	}
	return token, nil
}

func EncryptGitToken(ctx context.Context, secretService *secrets.Service, binding secrets.ResourceBinding, token string) (json.RawMessage, error) {
	if token == "" {
		return json.RawMessage(`null`), nil
	}
	if secretService == nil {
		return nil, ErrGitTokenCrypto
	}
	tokenBytes := []byte(token)
	defer clear(tokenBytes)
	envelope, err := secretService.SealResource(ctx, binding, tokenBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitTokenCrypto, err)
	}
	return json.Marshal(storedGitToken{Envelope: envelope})
}

// ParseGitTokenEnvelope validates ciphertext without decrypting it, allowing Deployment copies.
func ParseGitTokenEnvelope(tokenJSON json.RawMessage) (*secrets.Envelope, error) {
	if len(tokenJSON) == 0 || isJSONNull(tokenJSON) {
		return nil, nil
	}
	var stored storedGitToken
	if err := json.Unmarshal(tokenJSON, &stored); err != nil || stored.Envelope.FormatVersion == 0 {
		return nil, errors.New("git resource token must be re-submitted before use")
	}
	return &stored.Envelope, nil
}

func DecryptGitToken(ctx context.Context, secretService *secrets.Service, binding secrets.ResourceBinding, tokenJSON json.RawMessage) (string, error) {
	envelope, err := ParseGitTokenEnvelope(tokenJSON)
	if err != nil {
		return "", err
	}
	if envelope == nil {
		return "", nil
	}
	if secretService == nil {
		return "", ErrGitTokenCrypto
	}
	tokenBytes, err := secretService.OpenResource(ctx, binding, *envelope)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGitTokenCrypto, err)
	}
	// The returned string still contains the token after this buffer is cleared.
	defer clear(tokenBytes)
	return string(tokenBytes), nil
}
