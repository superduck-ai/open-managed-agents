package environments

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestResolveEnvironmentSandboxUsesCurrentBase(t *testing.T) {
	runner := &Runner{provider: e2bruntime.NewProvider(config.E2BConfig{Template: "current-base"})}
	for _, tc := range []struct {
		name         string
		config       string
		stored       string
		jobID        *int64
		wantTemplate string
		preinstalled bool
		wantError    bool
	}{
		{name: "unbuilt initialization script is rejected", config: `{"type":"cloud","init_script":"echo ready"}`, stored: "old-base", wantError: true},
		{name: "legacy base follows deployment", config: `{"type":"cloud"}`, stored: "old-base", wantTemplate: "current-base"},
		{name: "legacy packages follow deployment", config: `{"type":"cloud","packages":{"npm":["@larksuite/cli@1.0.97"]}}`, stored: "old-base", wantTemplate: "current-base"},
		{name: "empty binding follows deployment", config: `{"type":"cloud"}`, wantTemplate: "current-base"},
		{name: "pending prebuild follows deployment", config: `{"type":"cloud"}`, jobID: new(int64(42)), wantTemplate: "current-base"},
		{name: "completed prebuild remains pinned", config: `{"type":"cloud"}`, stored: "built-template", jobID: new(int64(42)), wantTemplate: "built-template", preinstalled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := db.Environment{Config: []byte(tc.config), ResolvedTemplate: tc.stored, BuildJobID: tc.jobID}
			resolution, preinstalled, err := runner.resolveEnvironmentSandbox(env, nil)
			if tc.wantError {
				if err == nil {
					t.Fatal("accepted environment without required prebuild")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if resolution.Template != tc.wantTemplate || preinstalled != tc.preinstalled {
				t.Fatalf("template=%q preinstalled=%v; want %q, %v", resolution.Template, preinstalled, tc.wantTemplate, tc.preinstalled)
			}
		})
	}
}
