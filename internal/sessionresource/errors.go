package sessionresource

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

var (
	errStoredFileResource       = errors.New("stored file resource is invalid")
	errStoredFileResourceType   = fmt.Errorf("stored file resource type must be %q", FileType)
	errStoredFileIDRequired     = errors.New("stored file resource file_id is required")
	errStoredFileResourceSource = fmt.Errorf("stored file resource source must be %q", sandboxmount.FileSource)
	errFileResourcePayload      = errors.New("file resource payload is invalid")
	errTooManyFileResources     = fmt.Errorf("at most %d managed-agent file resources are allowed", MaxFileResources)

	ErrGitTokenCrypto               = errors.New("git token cryptographic operation failed")
	errGitRepositoryURL             = errors.New("url must be an HTTPS repository URL without credentials, query, or fragment")
	errGitRepositoryPort            = errors.New("git repository URL must use HTTPS port 443")
	errGitRepositoryPath            = errors.New("url must contain an unambiguous repository path")
	errGitCheckoutObject            = errors.New("checkout must be an object")
	errGitCheckoutBranch            = errors.New("checkout.name must be a valid Git branch name and checkout.sha must be omitted")
	errGitCheckoutCommit            = errors.New("checkout.sha must be a full 40- or 64-character hexadecimal commit SHA and checkout.name must be omitted")
	errGitCheckoutType              = errors.New("checkout.type must be branch or commit")
	errGitMountPath                 = errors.New("git mount_path must be a directory below /workspace without control characters or backslashes")
	errGitMountPathReserved         = errors.New("git mount_path must not use .git, .claude, or .oma directories")
	errStoredGitResource            = errors.New("stored Git resource is invalid")
	errDuplicateGitRepository       = errors.New("git repository URLs must not be duplicated within resources")
	errOverlappingGitMountPaths     = errors.New("git resource mount_path values must not overlap")
	errGitTokenInputType            = errors.New("authorization_token must be a string or null")
	errGitTokenInputValue           = errors.New("authorization_token must not contain whitespace or control characters and must be at most 8192 bytes")
	errGitTokenResubmissionRequired = errors.New("git resource token must be re-submitted before use")
)

func gitMountPathError(err error) error {
	return fmt.Errorf("mount_path %w", err)
}

func gitTokenCryptoError(err error) error {
	return fmt.Errorf("%w: %w", ErrGitTokenCrypto, err)
}

func requiredFieldError(name string) error {
	return fmt.Errorf("%s is required", name)
}

func stringFieldTypeError(name string) error {
	return fmt.Errorf("%s must be a string", name)
}

func emptyFieldError(name string) error {
	return fmt.Errorf("%s must be non-empty", name)
}
