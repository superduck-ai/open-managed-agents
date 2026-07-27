package db

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValidateFilestorePath(t *testing.T) {
	t.Run("rejects unsafe or noncanonical paths", func(t *testing.T) {
		invalidPaths := []string{
			"",
			"relative/path",
			"/trailing/",
			"/double//slash",
			"/./file",
			"/parent/../file",
			"/nul\x00byte",
			string([]byte{'/', 0xff}),
			"/" + strings.Repeat("a", filestoreMaxPathBytes),
		}
		for _, entryPath := range invalidPaths {
			if err := validateFilestorePath(entryPath); !errors.Is(err, ErrPreconditionFailed) {
				t.Errorf("validateFilestorePath(%q) error = %v, want ErrPreconditionFailed", entryPath, err)
			}
		}
	})

	t.Run("accepts root and normalized absolute paths", func(t *testing.T) {
		validPaths := []string{"/", "/file.txt", "/reports/2026/七月.txt"}
		for _, entryPath := range validPaths {
			if err := validateFilestorePath(entryPath); err != nil {
				t.Errorf("validateFilestorePath(%q) error = %v", entryPath, err)
			}
		}
	})
}

func TestValidateFilestoreJSONObject(t *testing.T) {
	t.Run("rejects invalid JSON and non-objects", func(t *testing.T) {
		invalidValues := []json.RawMessage{
			json.RawMessage(`[]`),
			json.RawMessage(`"value"`),
			json.RawMessage(`{"missing":`),
		}
		for _, value := range invalidValues {
			if err := validateFilestoreJSONObject(value); !errors.Is(err, ErrPreconditionFailed) {
				t.Errorf("validateFilestoreJSONObject(%s) error = %v, want ErrPreconditionFailed", value, err)
			}
		}
	})

	t.Run("accepts omitted null and object values", func(t *testing.T) {
		validValues := []json.RawMessage{
			nil,
			json.RawMessage(`  null `),
			json.RawMessage(` {"intent":"read"} `),
		}
		for _, value := range validValues {
			if err := validateFilestoreJSONObject(value); err != nil {
				t.Errorf("validateFilestoreJSONObject(%s) error = %v", value, err)
			}
		}
	})

	t.Run("normalizes omitted values and trims objects", func(t *testing.T) {
		if got := string(filestoreJSONObject(json.RawMessage(` null `))); got != `{}` {
			t.Fatalf("filestoreJSONObject(null) = %q, want {}", got)
		}
		if got := string(filestoreJSONObject(json.RawMessage(` {"key":"value"} `))); got != `{"key":"value"}` {
			t.Fatalf("filestoreJSONObject(object) = %q", got)
		}
	})
}

func TestValidateFilestoreFileWrite(t *testing.T) {
	validBlob := FilestoreFileBlob{
		SizeBytes: 4,
		MediaType: "text/plain",
		MD5:       "098f6bcd4621d373cade4e832627b4f6",
		SHA256:    strings.Repeat("a", 64),
		S3Bucket:  "filestore",
		S3Key:     "objects/file",
	}

	t.Run("rejects root and incomplete blob metadata", func(t *testing.T) {
		if err := validateFilestoreFileWrite("/", validBlob); !errors.Is(err, ErrPreconditionFailed) {
			t.Fatalf("root file write error = %v, want ErrPreconditionFailed", err)
		}
		invalidBlob := validBlob
		invalidBlob.SHA256 = "short"
		if err := validateFilestoreFileWrite("/file", invalidBlob); !errors.Is(err, ErrPreconditionFailed) {
			t.Fatalf("invalid SHA-256 error = %v, want ErrPreconditionFailed", err)
		}
	})

	t.Run("accepts complete file metadata", func(t *testing.T) {
		if err := validateFilestoreFileWrite("/file", validBlob); err != nil {
			t.Fatalf("validateFilestoreFileWrite() error = %v", err)
		}
	})
}

func TestFilestorePathHelpers(t *testing.T) {
	t.Run("does not confuse sibling prefixes with descendants", func(t *testing.T) {
		if filestorePathIsDescendant("/reports", "/reports-old/file") {
			t.Fatal("sibling prefix reported as descendant")
		}
		if !filestorePathIsDescendant("/reports", "/reports/2026/file") {
			t.Fatal("nested path not reported as descendant")
		}
	})

	t.Run("builds parents from root to leaf", func(t *testing.T) {
		got := filestoreDirectoryChain("/reports/2026/july")
		want := []string{"/reports", "/reports/2026", "/reports/2026/july"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("filestoreDirectoryChain() = %#v, want %#v", got, want)
		}
		if parent := filestoreParentPath("/reports/file"); parent != "/reports" {
			t.Fatalf("filestoreParentPath() = %q, want /reports", parent)
		}
	})
}

func TestNormalizeFilestoreEntriesPageLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "defaults nonpositive limit", limit: 0, want: defaultFilestoreEntriesPageLimit},
		{name: "caps excessive limit", limit: maxFilestoreEntriesPageLimit + 1, want: maxFilestoreEntriesPageLimit},
		{name: "preserves valid limit", limit: 25, want: 25},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeFilestoreEntriesPageLimit(test.limit); got != test.want {
				t.Fatalf("normalizeFilestoreEntriesPageLimit(%d) = %d, want %d", test.limit, got, test.want)
			}
		})
	}
}

func TestBuildFilestoreEntriesPageQuery(t *testing.T) {
	filesystem := FilestoreFilesystem{WorkspaceUUID: "workspace-uuid", UUID: "filesystem-uuid"}

	t.Run("lists direct children without cursor", func(t *testing.T) {
		query, args := buildFilestoreEntriesPageQuery(filesystem, ListFilestoreEntriesPageParams{
			DirectoryPath: "/reports",
			Limit:         25,
		})
		if !strings.Contains(query, "and parent_path = :directory_path") ||
			strings.Contains(query, "(path, id) >") ||
			!strings.Contains(query, "limit :fetch_limit") {
			t.Fatalf("direct-child query = %q", query)
		}
		wantArgs := map[string]any{
			"workspace_uuid":  "workspace-uuid",
			"filesystem_uuid": "filesystem-uuid",
			"directory_path":  "/reports",
			"fetch_limit":     26,
		}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Fatalf("direct-child args = %#v, want %#v", args, wantArgs)
		}
	})

	t.Run("lists recursive descendants after cursor", func(t *testing.T) {
		query, args := buildFilestoreEntriesPageQuery(filesystem, ListFilestoreEntriesPageParams{
			DirectoryPath: "/reports",
			Recursive:     true,
			Limit:         25,
			Cursor:        &FilestoreEntryPageCursor{Path: "/reports/a", ID: 10},
		})
		if !strings.Contains(query, "left(path, char_length(:directory_prefix)) = :directory_prefix") ||
			!strings.Contains(query, "and (path, id) > (:cursor_path, :cursor_id)") ||
			!strings.Contains(query, "limit :fetch_limit") {
			t.Fatalf("recursive query = %q", query)
		}
		wantArgs := map[string]any{
			"workspace_uuid":   "workspace-uuid",
			"filesystem_uuid":  "filesystem-uuid",
			"directory_prefix": "/reports/",
			"cursor_path":      "/reports/a",
			"cursor_id":        int64(10),
			"fetch_limit":      26,
		}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Fatalf("recursive args = %#v, want %#v", args, wantArgs)
		}
	})

	t.Run("keeps root descendant prefix canonical", func(t *testing.T) {
		if got := filestoreDirectoryPrefix("/"); got != "/" {
			t.Fatalf("filestoreDirectoryPrefix(/) = %q, want /", got)
		}
	})
}

func TestFilestoreEntrySQLXRowEntry(t *testing.T) {
	t.Run("rejects malformed tag JSON", func(t *testing.T) {
		_, err := (filestoreEntryRow{TagsJSON: "not-json"}).entry()
		if err == nil {
			t.Fatal("entry() error = nil, want malformed tags error")
		}
	})

	t.Run("maps database row to domain entry", func(t *testing.T) {
		row := filestoreEntryRow{
			ID:                    7,
			UUID:                  "entry-uuid",
			ExternalID:            "file_7",
			OrganizationUUID:      "organization-uuid",
			WorkspaceUUID:         "workspace-uuid",
			FilesystemUUID:        "filesystem-uuid",
			Kind:                  FilestoreEntryKindFile,
			Path:                  "/reports/july.txt",
			Metadata:              []byte(`{"source":"test"}`),
			AuthorizationMetadata: []byte(`{}`),
			TagsJSON:              `["report","july"]`,
		}
		entry, err := row.entry()
		if err != nil {
			t.Fatalf("entry() error = %v", err)
		}
		if entry.ID != row.ID || entry.Path != row.Path || !reflect.DeepEqual(entry.Tags, []string{"report", "july"}) {
			t.Fatalf("entry() = %+v, want row identity and decoded tags", entry)
		}
		if string(entry.Metadata) != `{"source":"test"}` {
			t.Fatalf("entry metadata = %s", entry.Metadata)
		}
	})
}

func TestNewFilestoreEntryPage(t *testing.T) {
	entries := []FilestoreEntry{{ID: 1}, {ID: 2}, {ID: 3}}

	t.Run("trims lookahead entry", func(t *testing.T) {
		page := newFilestoreEntryPage(entries, 2)
		if !page.HasMore || len(page.Entries) != 2 || page.Entries[1].ID != 2 {
			t.Fatalf("page = %+v, want two entries with HasMore", page)
		}
	})

	t.Run("keeps complete final page", func(t *testing.T) {
		page := newFilestoreEntryPage(entries[:2], 2)
		if page.HasMore || len(page.Entries) != 2 {
			t.Fatalf("page = %+v, want complete final page", page)
		}
	})
}

func TestFilestoreObjectIdentityIncludesVersion(t *testing.T) {
	bucket := "filestore"
	key := "objects/file"
	version := "version-1"
	entry := FilestoreEntry{S3Bucket: &bucket, S3Key: &key, S3VersionID: &version}

	if !sameFilestoreObject(entry, FilestoreFileBlob{S3Bucket: bucket, S3Key: key, S3VersionID: version}) {
		t.Fatal("same exact object version was not recognized")
	}
	if sameFilestoreObject(entry, FilestoreFileBlob{S3Bucket: bucket, S3Key: key, S3VersionID: "version-2"}) {
		t.Fatal("different object version was recognized as identical")
	}
}

func TestVirtualFilestoreRoot(t *testing.T) {
	createdAt := time.Date(2026, time.July, 21, 1, 2, 3, 0, time.UTC)
	filesystem := FilestoreFilesystem{
		ID:               42,
		UUID:             "00000000-0000-0000-0000-000000000042",
		ExternalID:       "fs_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000007",
		WorkspaceUUID:    "00000000-0000-4000-8000-000000000008",
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}

	root := virtualFilestoreRoot(filesystem)
	if root.ID != 0 || root.Path != "/" || root.ParentPath != nil || root.Kind != FilestoreEntryKindDirectory {
		t.Fatalf("virtual root = %#v", root)
	}
	if root.OrganizationUUID != filesystem.OrganizationUUID ||
		root.WorkspaceUUID != filesystem.WorkspaceUUID || root.FilesystemUUID != filesystem.UUID ||
		root.ExternalID != filesystem.ExternalID {
		t.Fatalf("virtual root scope = %#v, want filesystem %#v", root, filesystem)
	}
}

func TestCreateFilestoreFilesystemWithGeneratedID(t *testing.T) {
	t.Run("failure exhausts collision retries", func(t *testing.T) {
		generateCalls := 0
		insertCalls := 0
		_, err := createFilestoreFilesystemWithGeneratedID(
			func() (string, error) {
				generateCalls++
				return "claude_chat_collision", nil
			},
			func(string) (FilestoreFilesystem, bool, error) {
				insertCalls++
				return FilestoreFilesystem{}, false, nil
			},
		)
		if !errors.Is(err, ErrDuplicate) {
			t.Fatalf("createFilestoreFilesystemWithGeneratedID() error = %v, want ErrDuplicate", err)
		}
		if generateCalls != filestoreFilesystemIDMaxAttempts || insertCalls != filestoreFilesystemIDMaxAttempts {
			t.Fatalf("retry calls = generate %d, insert %d; want %d", generateCalls, insertCalls, filestoreFilesystemIDMaxAttempts)
		}
	})

	t.Run("failure returns random source error", func(t *testing.T) {
		wantErr := errors.New("random source unavailable")
		insertCalled := false
		_, err := createFilestoreFilesystemWithGeneratedID(
			func() (string, error) { return "", wantErr },
			func(string) (FilestoreFilesystem, bool, error) {
				insertCalled = true
				return FilestoreFilesystem{}, false, nil
			},
		)
		if !errors.Is(err, wantErr) || insertCalled {
			t.Fatalf("create result = error %v, insert called %v", err, insertCalled)
		}
	})

	t.Run("success regenerates after collision", func(t *testing.T) {
		generated := []string{"claude_chat_first", "claude_chat_second"}
		generateIndex := 0
		filesystem, err := createFilestoreFilesystemWithGeneratedID(
			func() (string, error) {
				value := generated[generateIndex]
				generateIndex++
				return value, nil
			},
			func(externalID string) (FilestoreFilesystem, bool, error) {
				if externalID == generated[0] {
					return FilestoreFilesystem{}, false, nil
				}
				return FilestoreFilesystem{ExternalID: externalID}, true, nil
			},
		)
		if err != nil {
			t.Fatalf("createFilestoreFilesystemWithGeneratedID() error = %v", err)
		}
		if filesystem.ExternalID != generated[1] || generateIndex != 2 {
			t.Fatalf("filesystem = %#v, generated IDs = %d", filesystem, generateIndex)
		}
	})
}
