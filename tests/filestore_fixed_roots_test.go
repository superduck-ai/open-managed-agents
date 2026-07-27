package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestFilestoreFixedRootsRejectGenericDirectoryMutation(t *testing.T) {
	fixture := newWorkspaceStorageFixture(t)
	ctx := context.Background()
	makeFilestoreTestDirectory(t, fixture, "/outside")
	makeFilestoreTestDirectory(t, fixture, "/outputs/cross-root")

	t.Run("rejects moving fixed roots", func(t *testing.T) {
		for _, rootPath := range []string{"/outputs", "/uploads", "/transcripts", "/tool_results"} {
			_, err := fixture.app.db.MoveFilestoreDirectory(ctx, db.MoveFilestoreDirectoryInput{
				WorkspaceID:     fixture.workspaceID,
				FilesystemID:    fixture.filesystem.ID,
				SourcePath:      rootPath,
				DestinationPath: rootPath + "-renamed",
			})
			if !errors.Is(err, db.ErrFilestoreInvalidMove) {
				t.Fatalf("MoveFilestoreDirectory(%q) error = %v, want ErrFilestoreInvalidMove", rootPath, err)
			}
		}
	})

	t.Run("rejects removing fixed roots", func(t *testing.T) {
		for _, rootPath := range []string{"/outputs", "/uploads", "/transcripts", "/tool_results"} {
			_, err := fixture.app.db.RemoveFilestoreDirectory(ctx, db.RemoveFilestoreDirectoryInput{
				WorkspaceID:  fixture.workspaceID,
				FilesystemID: fixture.filesystem.ID,
				Path:         rootPath,
				Recursive:    true,
			})
			if !errors.Is(err, db.ErrFilestoreInvalidMove) {
				t.Fatalf("RemoveFilestoreDirectory(%q) error = %v, want ErrFilestoreInvalidMove", rootPath, err)
			}
		}
	})

	t.Run("rejects covering a fixed destination root", func(t *testing.T) {
		_, err := fixture.app.db.MoveFilestoreDirectory(ctx, db.MoveFilestoreDirectoryInput{
			WorkspaceID:     fixture.workspaceID,
			FilesystemID:    fixture.filesystem.ID,
			SourcePath:      "/outside",
			DestinationPath: "/uploads",
		})
		if !errors.Is(err, db.ErrFilestoreInvalidMove) {
			t.Fatalf("MoveFilestoreDirectory() error = %v, want ErrFilestoreInvalidMove", err)
		}
	})

	t.Run("rejects crossing fixed root namespaces", func(t *testing.T) {
		_, err := fixture.app.db.MoveFilestoreDirectory(ctx, db.MoveFilestoreDirectoryInput{
			WorkspaceID:     fixture.workspaceID,
			FilesystemID:    fixture.filesystem.ID,
			SourcePath:      "/outputs/cross-root",
			DestinationPath: "/uploads/cross-root",
		})
		if !errors.Is(err, db.ErrFilestoreInvalidMove) {
			t.Fatalf("MoveFilestoreDirectory() error = %v, want ErrFilestoreInvalidMove", err)
		}
	})

	t.Run("rejects entering or leaving a fixed root namespace", func(t *testing.T) {
		for _, move := range []db.MoveFilestoreDirectoryInput{
			{
				WorkspaceID:     fixture.workspaceID,
				FilesystemID:    fixture.filesystem.ID,
				SourcePath:      "/outside",
				DestinationPath: "/tool_results/outside",
			},
			{
				WorkspaceID:     fixture.workspaceID,
				FilesystemID:    fixture.filesystem.ID,
				SourcePath:      "/outputs/cross-root",
				DestinationPath: "/cross-root",
			},
		} {
			if _, err := fixture.app.db.MoveFilestoreDirectory(ctx, move); !errors.Is(err, db.ErrFilestoreInvalidMove) {
				t.Fatalf(
					"MoveFilestoreDirectory(%q, %q) error = %v, want ErrFilestoreInvalidMove",
					move.SourcePath,
					move.DestinationPath,
					err,
				)
			}
		}
	})

	t.Run("allows mutations within one fixed root", func(t *testing.T) {
		makeFilestoreTestDirectory(t, fixture, "/outputs/drafts")
		moved, err := fixture.app.db.MoveFilestoreDirectory(ctx, db.MoveFilestoreDirectoryInput{
			WorkspaceID:     fixture.workspaceID,
			FilesystemID:    fixture.filesystem.ID,
			SourcePath:      "/outputs/drafts",
			DestinationPath: "/outputs/published",
		})
		if err != nil {
			t.Fatalf("MoveFilestoreDirectory() error = %v", err)
		}
		if moved.Entry.Path != "/outputs/published" {
			t.Fatalf("moved path = %q, want /outputs/published", moved.Entry.Path)
		}
		if _, err := fixture.app.db.RemoveFilestoreDirectory(ctx, db.RemoveFilestoreDirectoryInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         moved.Entry.Path,
		}); err != nil {
			t.Fatalf("RemoveFilestoreDirectory() error = %v", err)
		}
	})
}

func makeFilestoreTestDirectory(t *testing.T, fixture workspaceStorageFixture, entryPath string) {
	t.Helper()
	if _, err := fixture.app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
		WorkspaceID:  fixture.workspaceID,
		FilesystemID: fixture.filesystem.ID,
		Path:         entryPath,
	}); err != nil {
		t.Fatalf("MakeFilestoreDirectory(%q) error = %v", entryPath, err)
	}
}
