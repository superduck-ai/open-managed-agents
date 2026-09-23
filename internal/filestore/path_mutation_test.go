package filestore

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPathRouterMutationFailures(t *testing.T) {
	t.Parallel()

	router := pathRouter{
		memory:   &memoryPathBackend{},
		readOnly: []readOnlyPathBackend{&skillArchivePathBackend{}},
	}
	tests := []struct {
		name      string
		operation mutationOperation
		paths     []string
		status    int
		code      string
	}{
		{"read only root", mutationSinglePath, []string{"/skills"}, http.StatusForbidden, "permission_denied"},
		{"read only descendant", mutationSinglePath, []string{"/skills/demo/SKILL.md"}, http.StatusForbidden, "permission_denied"},
		{"read only destination precedes memory transfer", mutationFileTransfer, []string{"/memory/team/a.txt", "/skills/demo/a.txt"}, http.StatusForbidden, "permission_denied"},
		{"read only source precedes memory transfer", mutationFileTransfer, []string{"/skills/demo/a.txt", "/memory/team/a.txt"}, http.StatusForbidden, "permission_denied"},
		{"read only directory precedes memory transfer", mutationDirectoryTransfer, []string{"/memory/team/notes", "/skills/demo"}, http.StatusForbidden, "permission_denied"},
		{"memory to persistent", mutationFileTransfer, []string{"/memory/team/a.txt", "/outputs/a.txt"}, http.StatusBadRequest, "invalid_argument"},
		{"persistent to memory", mutationFileTransfer, []string{"/outputs/a.txt", "/memory/team/a.txt"}, http.StatusBadRequest, "invalid_argument"},
		{"different stores", mutationFileTransfer, []string{"/memory/team/a.txt", "/memory/other/a.txt"}, http.StatusBadRequest, "invalid_argument"},
		{"virtual directory", mutationDirectoryTransfer, []string{"/memory/team/a", "/memory/team/b"}, http.StatusConflict, "failed_precondition"},
		{"directory to persistent", mutationDirectoryTransfer, []string{"/memory/team/a", "/outputs/a"}, http.StatusConflict, "failed_precondition"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend, apiErr := router.mutationBackendFor(test.operation, test.paths...)
			assertServiceAPIError(t, apiErr, test.status, test.code)
			if backend != nil {
				t.Fatal("rejected mutation must not return a writable backend")
			}
		})
	}
}

func TestPathRouterSelectsMutationBackend(t *testing.T) {
	t.Parallel()

	memory := &memoryPathBackend{}
	router := pathRouter{memory: memory, readOnly: []readOnlyPathBackend{&skillArchivePathBackend{}}}
	tests := []struct {
		name      string
		operation mutationOperation
		paths     []string
		want      writablePathBackend
	}{
		{"memory file", mutationSinglePath, []string{"/memory/team/a.txt"}, memory},
		{"memory root", mutationSinglePath, []string{"/memory/team"}, memory},
		{"same store transfer", mutationFileTransfer, []string{"/memory/team/a.txt", "/memory/team/b.txt"}, memory},
		{"ordinary file", mutationSinglePath, []string{"/outputs/a.txt"}, nil},
		{"memory parent", mutationSinglePath, []string{"/memory"}, nil},
		{"parent file", mutationSinglePath, []string{"/memory/MEMORY.md"}, nil},
		{"ordinary transfer", mutationFileTransfer, []string{"/outputs/a.txt", "/outputs/b.txt"}, nil},
		{"ordinary directory", mutationDirectoryTransfer, []string{"/outputs/a", "/outputs/b"}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend, apiErr := router.mutationBackendFor(test.operation, test.paths...)
			if apiErr != nil || backend != test.want {
				t.Fatalf("mutationBackendFor() = %T, %v; want %T, nil", backend, apiErr, test.want)
			}
		})
	}
}

func TestServiceRoutesMemoryMutationsWithExplicitDependency(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	// This database intentionally only implements ordinary Filestore persistence.
	// Any accidental fallback to ordinary mutation methods panics in the fake.
	database := &fakeServiceDatabase{
		getFilesystemFn: func(context.Context, string, string) (db.FilestoreFilesystem, error) {
			return filesystem, nil
		},
	}
	memories := &recordingMemoryStore{mounts: []db.SessionMemoryMount{{
		MemoryStoreUUID:       "store-uuid",
		MemoryStoreExternalID: "memstore_team",
		MountPath:             "/mnt/memory/team",
		Access:                "read_only",
	}}}
	service := NewService(config.Config{}, database, memories, &fakeServiceBlobStore{})
	ctx, principal := context.Background(), serviceTestPrincipal()
	path := "/memory/team/notes/a.txt"
	transfer := copyMoveFileRequest{FilesystemID: filesystem.ExternalID, Source: path, Destination: "/memory/team/notes/b.txt"}
	tests := []struct {
		name string
		run  func() *apiError
	}{
		{"make directory", func() *apiError {
			_, err := service.MakeDirectory(ctx, principal, makeDirectoryRequest{FilesystemID: filesystem.ExternalID, Path: path})
			return err
		}},
		{"remove directory", func() *apiError {
			return service.RemoveDirectory(ctx, principal, removeDirectoryRequest{FilesystemID: filesystem.ExternalID, Path: path})
		}},
		{"create file", func() *apiError {
			_, err := service.CreateFile(ctx, principal, createFileParams{FilesystemID: filesystem.ExternalID, Path: path, MediaType: "text/plain"}, strings.NewReader("body"))
			return err
		}},
		{"remove file", func() *apiError {
			return service.RemoveFile(ctx, principal, pathRequest{FilesystemID: filesystem.ExternalID, Path: path})
		}},
		{"copy file", func() *apiError {
			_, err := service.CopyFile(ctx, principal, transfer)
			return err
		}},
		{"move file", func() *apiError {
			_, err := service.MoveFile(ctx, principal, transfer)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertServiceAPIError(t, test.run(), http.StatusForbidden, "permission_denied")
		})
	}
	_, apiErr := service.MoveDirectory(ctx, principal, moveDirectoryRequest{
		FilesystemID: filesystem.ExternalID, Source: "/memory/team/notes", Destination: "/memory/team/archive",
	})
	assertServiceAPIError(t, apiErr, http.StatusConflict, "failed_precondition")
}
