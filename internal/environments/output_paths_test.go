package environments

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionOutputsAlias(t *testing.T) {
	for _, state := range []string{"file", "nonempty directory", "wrong symlink", "empty directory", "absent", "existing alias"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "user data's outputs")
			alias := filepath.Join(root, "session", "outputs")
			if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "file":
				if err := os.WriteFile(alias, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "nonempty directory", "empty directory":
				if err := os.Mkdir(alias, 0755); err != nil {
					t.Fatal(err)
				}
				if state == "nonempty directory" {
					if err := os.WriteFile(filepath.Join(alias, "keep.txt"), []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "wrong symlink", "existing alias":
				linkTarget := target
				if state == "wrong symlink" {
					linkTarget = filepath.Join(root, "other")
				}
				if err := os.Symlink(linkTarget, alias); err != nil {
					t.Fatal(err)
				}
			}
			command := sessionOutputsAliasCommand(target, alias)
			output, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
			if state == "file" || state == "nonempty directory" || state == "wrong symlink" {
				if err == nil {
					t.Fatal("conflicting output path accepted")
				}
				if _, err := os.Lstat(alias); err != nil {
					t.Fatalf("conflicting path removed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("create alias: %v: %s", err, output)
			}
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(alias, "result.txt"), []byte("result"), 0600); err != nil {
				t.Fatal(err)
			}
			if contents, err := os.ReadFile(filepath.Join(target, "result.txt")); err != nil || string(contents) != "result" {
				t.Fatalf("alias write did not reach output mount: %q, %v", contents, err)
			}
			if output, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput(); err != nil {
				t.Fatalf("alias setup is not idempotent: %v: %s", err, output)
			}
		})
	}
}

func TestAgentScoreContractAliases(t *testing.T) {
	t.Run("metadata", func(t *testing.T) {
		for _, metadata := range []string{"", "null", "{", `{"evaluation_role":"case"}`, `{"evaluation_role":1}`, `{"experiment_id":"evalexp_1"}`} {
			if sessionRequestsAgentScorePaths([]byte(metadata)) {
				t.Fatalf("metadata %s requested score paths", metadata)
			}
		}
		if !sessionRequestsAgentScorePaths([]byte(`{"evaluation_role":"agent_score","experiment_id":"evalexp_1"}`)) {
			t.Fatal("agent_score metadata did not request score paths")
		}
	})
	t.Run("commands", func(t *testing.T) {
		commands := agentScoreContractAliasCommands()
		if len(commands) != 2 ||
			commands[0] != sessionOutputsAliasCommand(rcloneUploadsDestination+"/data", "/data") ||
			commands[1] != sessionOutputsAliasCommand(rcloneOutputsDestination, "/output") {
			t.Fatalf("score contract aliases = %#v", commands)
		}
	})
	t.Run("layout", func(t *testing.T) {
		root := t.TempDir()
		uploadsData := filepath.Join(root, "mnt", "session", "uploads", "data")
		if err := os.MkdirAll(filepath.Join(uploadsData, "session"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(uploadsData, "session", "result.json"), []byte(`{"ok":true}`), 0600); err != nil {
			t.Fatal(err)
		}
		outputs := filepath.Join(root, "mnt", "user-data", "outputs")
		if err := os.MkdirAll(outputs, 0755); err != nil {
			t.Fatal(err)
		}
		dataAlias := filepath.Join(root, "data")
		outputAlias := filepath.Join(root, "output")
		for _, command := range []string{
			sessionOutputsAliasCommand(uploadsData, dataAlias),
			sessionOutputsAliasCommand(outputs, outputAlias),
		} {
			if output, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput(); err != nil {
				t.Fatalf("alias: %v: %s", err, output)
			}
		}
		if got, err := os.ReadFile(filepath.Join(dataAlias, "session", "result.json")); err != nil || string(got) != `{"ok":true}` {
			t.Fatalf("result.json via data alias = %q, %v", got, err)
		}
		if err := os.WriteFile(filepath.Join(outputAlias, "score.json"), []byte(`{"score":1}`), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(outputs, "score.json")); err != nil || string(got) != `{"score":1}` {
			t.Fatalf("score.json via output alias = %q, %v", got, err)
		}
	})
}

func TestAgentScoreContractAliasStartup(t *testing.T) {
	t.Run("alias failure prevents mount startup", func(t *testing.T) {
		provider := &rcloneTestProvider{runErrors: []error{nil, errors.New("alias failed")}}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", rcloneFilestoreLaunch{ScoreContractAliases: true})
		if !errors.Is(err, errScoreContractAliasCreate) || provider.backgroundCalls != 0 || len(provider.writes) != 0 {
			t.Fatalf("score alias failure did not stop startup: error=%v, provider=%+v", err, provider)
		}
		if len(provider.runCommands) != 2 || provider.runCommands[1] != agentScoreContractAliasCommands()[0] {
			t.Fatalf("score alias commands = %v", provider.runCommands)
		}
	})
	t.Run("aliases ready before mount startup", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		launch := rcloneFilestoreLaunch{ConfigPayload: []byte(`{"mounts":[]}`), ScoreContractAliases: true}
		if err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", launch); err != nil {
			t.Fatal(err)
		}
		commands := agentScoreContractAliasCommands()
		if len(provider.runCommands) < 3 ||
			provider.runCommands[0] != sessionOutputsAliasCommand(rcloneOutputsDestination, sessionOutputsPath) ||
			provider.runCommands[1] != commands[0] ||
			provider.runCommands[2] != commands[1] {
			t.Fatalf("score contract aliases not prepared before mount: %v", provider.runCommands)
		}
	})
	t.Run("ordinary session omits contract aliases", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		if err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", rcloneFilestoreLaunch{ConfigPayload: []byte(`{"mounts":[]}`)}); err != nil {
			t.Fatal(err)
		}
		for _, command := range provider.runCommands {
			if command == agentScoreContractAliasCommands()[0] || command == agentScoreContractAliasCommands()[1] {
				t.Fatalf("ordinary session created score contract aliases: %v", provider.runCommands)
			}
		}
	})
}

func TestSessionOutputsAliasStartup(t *testing.T) {
	t.Run("failure prevents mount startup", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true, runErrors: []error{errors.New("alias failed")}}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", rcloneFilestoreLaunch{})
		if !errors.Is(err, errOutputsAliasCreate) || provider.backgroundCalls != 0 || len(provider.writes) != 0 {
			t.Fatalf("alias failure did not stop startup: error=%v, provider=%+v", err, provider)
		}
	})
	t.Run("alias ready before mount startup", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		if err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", rcloneFilestoreLaunch{}); err != nil {
			t.Fatal(err)
		}
		if len(provider.runCommands) == 0 || provider.runCommands[0] != sessionOutputsAliasCommand(rcloneOutputsDestination, sessionOutputsPath) {
			t.Fatalf("output alias not prepared: %v", provider.runCommands)
		}
		prompt, err := managedAgentEnvironmentPrompt(nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, "/mnt/session/outputs") {
			t.Fatal("environment prompt does not advertise the standard output path")
		}
	})
}
