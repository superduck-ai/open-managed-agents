package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisionDoctorRequiresExplicitModelConfiguration(t *testing.T) {
	t.Setenv("VERIFY_BE_VISION_PROVIDERS", "")
	t.Setenv("VERIFY_BE_VISION_MODEL", "vision-model")
	_, err := doctor(context.Background(), t.TempDir(), "", "chat.brain-images", 18080)
	if err == nil || !strings.Contains(err.Error(), "explicit valid") {
		t.Fatalf("missing file error=%v", err)
	}
	file := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(file, []byte("version: 1\nproviders:\n  - name: vision\n    api_backend: messages\n    base_url: https://models.example.com\n    api_key: private-test-key\n    model_ids: [vision-model]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VERIFY_BE_VISION_PROVIDERS", file)
	t.Setenv("VERIFY_BE_VISION_MODEL", "unknown")
	_, err = doctor(context.Background(), t.TempDir(), "", "chat.brain-images", 18080)
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("unknown model error=%v", err)
	}
	t.Setenv("VERIFY_BE_VISION_MODEL", "vision-model")
	t.Setenv("OMA_BRAIN_BINARY", "")
	result, err := doctor(context.Background(), t.TempDir(), "", "chat.brain-images", 18080)
	if err == nil || result.Versions["vision_model"] != "vision-model" || strings.Contains(err.Error(), "private-test-key") {
		t.Fatalf("resolved vision doctor result=%+v error=%v", result, err)
	}
}
