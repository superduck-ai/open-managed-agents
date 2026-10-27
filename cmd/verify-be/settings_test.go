package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerImageRejectsInvalidLocalSettings(t *testing.T) {
	for _, data := range []string{`{`, `{}`, `{"worker_image":" "}`, `{"worker_image":12}`, `private.example/image`} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, localSettingsFile), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := resolveWorkerImage(root, "", "")
			if err == nil {
				t.Fatal("invalid local settings accepted")
			}
			if strings.Contains(err.Error(), "private.example") {
				t.Fatal("settings content leaked in diagnostic")
			}
		})
	}
}

func TestWorkerImagePrecedence(t *testing.T) {
	root := t.TempDir()
	if image, err := resolveWorkerImage(root, "", ""); err != nil || image != defaultWorker {
		t.Fatalf("fallback=%q error=%v", image, err)
	}
	if err := os.WriteFile(filepath.Join(root, localSettingsFile), []byte(`{"worker_image":" local.example/worker:test "}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ flag, env, want string }{
		{want: "local.example/worker:test"},
		{env: "env.example/worker:test", want: "env.example/worker:test"},
		{flag: " flag.example/worker:test ", env: "env.example/worker:test", want: "flag.example/worker:test"},
	} {
		image, err := resolveWorkerImage(root, tc.flag, tc.env)
		if err != nil || image != tc.want {
			t.Fatalf("image=%q want=%q error=%v", image, tc.want, err)
		}
	}
}
