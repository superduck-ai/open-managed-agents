package db

import (
	"errors"
	"testing"
)

func TestValidateFilestoreDirectoryMoveRoots(t *testing.T) {
	rejected := []struct {
		name            string
		sourcePath      string
		destinationPath string
	}{
		{"fixed source root", "/outputs", "/outputs-renamed"},
		{"fixed destination root", "/outside", "/uploads"},
		{"crosses fixed roots", "/outputs/reports", "/uploads/reports"},
		{"leaves fixed root", "/transcripts/archive", "/archive"},
		{"enters fixed root", "/archive", "/tool_results/archive"},
	}
	for _, test := range rejected {
		t.Run("rejects "+test.name, func(t *testing.T) {
			err := validateFilestoreDirectoryMoveRoots(test.sourcePath, test.destinationPath)
			if !errors.Is(err, ErrFilestoreInvalidMove) {
				t.Fatalf("validate move roots error = %v, want ErrFilestoreInvalidMove", err)
			}
		})
	}

	allowed := []struct {
		name            string
		sourcePath      string
		destinationPath string
	}{
		{"same fixed root", "/outputs/drafts", "/outputs/published"},
		{"outside fixed roots", "/drafts", "/published"},
	}
	for _, test := range allowed {
		t.Run("allows "+test.name, func(t *testing.T) {
			if err := validateFilestoreDirectoryMoveRoots(test.sourcePath, test.destinationPath); err != nil {
				t.Fatalf("validate move roots error = %v", err)
			}
		})
	}
}

func TestValidateFilestoreDirectoryRemovalRoot(t *testing.T) {
	for _, rootPath := range filestoreFixedRootPaths {
		t.Run("rejects "+rootPath, func(t *testing.T) {
			err := validateFilestoreDirectoryRemovalRoot(rootPath)
			if !errors.Is(err, ErrFilestoreInvalidMove) {
				t.Fatalf("validate removal root error = %v, want ErrFilestoreInvalidMove", err)
			}
		})
	}

	for _, entryPath := range []string{"/outputs/archive", "/archive"} {
		t.Run("allows "+entryPath, func(t *testing.T) {
			if err := validateFilestoreDirectoryRemovalRoot(entryPath); err != nil {
				t.Fatalf("validate removal root error = %v", err)
			}
		})
	}
}
