package storage

import (
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestNewObjectStore(t *testing.T) {
	t.Run("failure unsupported type", func(t *testing.T) {
		_, err := New(config.StorageConfig{Type: "filesystem"})
		if err == nil || !strings.Contains(err.Error(), "unsupported object storage type") {
			t.Fatalf("New() error = %v, want unsupported type error", err)
		}
	})

	t.Run("success s3", func(t *testing.T) {
		store, err := New(config.StorageConfig{
			Type: config.StorageTypeS3,
			S3: config.S3Config{
				Endpoint:        "http://localhost:9000",
				Bucket:          "test-bucket",
				Region:          "us-east-1",
				AccessKeyID:     "test-access-key",
				SecretAccessKey: "test-secret-key",
				ForcePathStyle:  true,
			},
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if store.Bucket() != "test-bucket" {
			t.Fatalf("Bucket() = %q, want test-bucket", store.Bucket())
		}
	})
}
