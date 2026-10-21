package devinit

import (
	"os/exec"
	"testing"
)

func lookPathGit() (string, error) {
	return exec.LookPath("git")
}

func runGitInit(dir string) error {
	command := exec.Command("git", "init", "-q")
	command.Dir = dir
	return command.Run()
}

func TestRegistryRequiresGitRepo(t *testing.T) {
	if _, err := lookPathGit(); err != nil {
		t.Skip("git not available")
	}
	if _, err := OpenRegistry(t.TempDir()); err == nil {
		t.Fatal("OpenRegistry() error = nil outside a git repository, want failure")
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := lookPathGit(); err != nil {
		t.Skip("git not available")
	}
	if err := runGitInit(dir); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
}
