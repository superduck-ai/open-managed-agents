package environments

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestMemoryRootProtectionStartup(t *testing.T) {
	mounts := []memoryRuntimeMount{{Name: "owned", MountPath: "/mnt/memory/owned", Slug: "owned", Access: "read_write"}}
	launch := rcloneFilestoreLaunch{ConfigPayload: []byte(`{"mounts":[]}`), MemoryMounts: mounts}
	t.Run("seal failure prevents startup", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true, runErrors: []error{nil, nil, nil, nil, errors.New("secret provider detail")}}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", launch)
		if err == nil || !strings.Contains(err.Error(), "memory mount root protection failed") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("root protection failure must stop startup with sanitized error: %v", err)
		}
	})
	t.Run("bind before children and seal after ready and index", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		if err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", launch); err != nil {
			t.Fatal(err)
		}
		if len(provider.runCommands) != 5 || provider.runCommands[0] != "mkdir -p '/mnt/memory' && mount --bind '/mnt/memory' '/mnt/memory'" || provider.runCommands[4] != "mount -o remount,bind,ro '/mnt/memory'" {
			t.Fatalf("missing nonrecursive parent protection around child mount startup: %v", provider.runCommands)
		}
		if provider.fileData(memoryMarkdownSandboxPath) != renderMemoryMarkdown(mounts) || provider.backgroundCalls != 1 {
			t.Fatal("index or child mount startup lost")
		}
	})
}
