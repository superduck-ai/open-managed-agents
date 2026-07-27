package filestore

import (
	"net/http"
	"testing"
)

func TestPathRouterRejectsReadOnlyNamespaceMutation(t *testing.T) {
	t.Parallel()

	router := pathRouter{
		persistent: &persistentPathBackend{},
		readOnly:   []readOnlyPathBackend{&skillArchivePathBackend{}},
	}
	for _, paths := range [][]string{
		{"/skills"},
		{"/skills/demo/SKILL.md"},
		{"/outputs/a.txt", "/skills/demo/a.txt"},
		{"/skills/demo/a.txt", "/outputs/a.txt"},
	} {
		apiErr := router.authorizeMutation(paths...)
		assertServiceAPIError(t, apiErr, http.StatusForbidden, "permission_denied")
	}
	if apiErr := router.authorizeMutation("/outputs/a.txt", "/outputs/b.txt"); apiErr != nil {
		t.Fatalf("authorizeMutation() error = %v, want nil", apiErr)
	}
}

func TestPathRouterSelectsReadBackend(t *testing.T) {
	t.Parallel()

	persistent := &persistentPathBackend{}
	skills := &skillArchivePathBackend{}
	router := pathRouter{
		persistent: persistent,
		readOnly:   []readOnlyPathBackend{skills},
	}
	tests := []struct {
		name      string
		operation readOperation
		path      string
		want      pathBackend
	}{
		{
			name:      "skill root list",
			operation: readOperationListDirectory,
			path:      "/skills",
			want:      skills,
		},
		{
			name:      "skill descendant list",
			operation: readOperationListDirectory,
			path:      "/skills/demo",
			want:      skills,
		},
		{
			name:      "skill descendant file",
			operation: readOperationFile,
			path:      "/skills/demo/SKILL.md",
			want:      skills,
		},
		{
			name:      "skill descendant metadata",
			operation: readOperationMetadata,
			path:      "/skills/demo/SKILL.md",
			want:      skills,
		},
		{
			name:      "skill root file stays persistent",
			operation: readOperationFile,
			path:      "/skills",
			want:      persistent,
		},
		{
			name:      "skill root metadata stays persistent",
			operation: readOperationMetadata,
			path:      "/skills",
			want:      persistent,
		},
		{
			name:      "ordinary path",
			operation: readOperationMetadata,
			path:      "/outputs/report.txt",
			want:      persistent,
		},
		{
			name:      "similar prefix is ordinary",
			operation: readOperationListDirectory,
			path:      "/skills-old",
			want:      persistent,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := router.backendFor(test.operation, test.path); got != test.want {
				t.Fatalf("backendFor(%q) = %T, want %T", test.path, got, test.want)
			}
		})
	}
}
