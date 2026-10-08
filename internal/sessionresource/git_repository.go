package sessionresource

import (
	"context"
	"encoding/json"
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
		return errGitRepositoryURL
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return errGitRepositoryPort
	}
	repositoryPath := strings.TrimRight(parsed.Path, "/")
	if repositoryPath == "" || path.Clean(repositoryPath) != repositoryPath || parsed.RawPath != "" || strings.Contains(repositoryPath, "\\") {
		return errGitRepositoryPath
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
		return nil, errGitCheckoutObject
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
			return errGitCheckoutBranch
		}
	case "commit":
		if checkout.Name != "" || !gitCommitSHA.MatchString(checkout.SHA) {
			return errGitCheckoutCommit
		}
		checkout.SHA = strings.ToLower(checkout.SHA)
	default:
		return errGitCheckoutType
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
		return gitMountPathError(err)
	}
	if !strings.HasPrefix(current, "/workspace/") || strings.Contains(current, "\\") || strings.ContainsFunc(current, unicode.IsControl) {
		return errGitMountPath
	}
	for _, part := range strings.Split(strings.TrimPrefix(current, "/workspace/"), "/") {
		if part == ".git" || part == ".claude" || part == ".oma" {
			return errGitMountPathReserved
		}
	}
	return nil
}

// ParseStoredGitRepositorySpec rejects missing persisted fields instead of applying defaults.
func ParseStoredGitRepositorySpec(raw json.RawMessage) (GitRepositorySpec, error) {
	var spec GitRepositorySpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return GitRepositorySpec{}, errStoredGitResource
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
			return errDuplicateGitRepository
		}
		repositories[key] = true
		for _, other := range specs[:i] {
			if spec.MountPath == other.MountPath || filestorepath.IsDescendant(spec.MountPath, other.MountPath) || filestorepath.IsDescendant(other.MountPath, spec.MountPath) {
				return errOverlappingGitMountPaths
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
			return "", errGitTokenInputType
		}
	}
	if len(token) > 8192 || strings.ContainsFunc(token, unicode.IsSpace) || strings.ContainsFunc(token, unicode.IsControl) {
		return "", errGitTokenInputValue
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
		return nil, gitTokenCryptoError(err)
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
		return nil, errGitTokenResubmissionRequired
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
		return "", gitTokenCryptoError(err)
	}
	// The returned string still contains the token after this buffer is cleared.
	defer clear(tokenBytes)
	return string(tokenBytes), nil
}
