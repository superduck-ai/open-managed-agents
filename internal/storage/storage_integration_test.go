package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestS3CompatibleIntegration(t *testing.T) {
	endpoint := os.Getenv("OMA_S3_INTEGRATION_ENDPOINT")
	if endpoint == "" {
		t.Skip("set OMA_S3_INTEGRATION_ENDPOINT to run S3-compatible integration tests")
	}
	bucketName := envOrDefault("OMA_S3_INTEGRATION_BUCKET", "claude-files")
	client, err := New(config.StorageConfig{
		Type: config.StorageTypeS3,
		S3: config.S3Config{
			Endpoint:        endpoint,
			Bucket:          bucketName,
			Region:          envOrDefault("OMA_S3_INTEGRATION_REGION", "us-east-1"),
			AccessKeyID:     envOrDefault("OMA_S3_INTEGRATION_ACCESS_KEY_ID", "minioadmin"),
			SecretAccessKey: envOrDefault("OMA_S3_INTEGRATION_SECRET_ACCESS_KEY", "minioadmin"),
			ForcePathStyle:  true,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	store, err := client.ForBucket(bucketName)
	if err != nil {
		t.Fatalf("ForBucket(%q) error = %v", bucketName, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() first call error = %v", err)
	}
	registerIntegrationBucketCleanup(t, store)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() second call error = %v", err)
	}

	t.Run("known small object", func(t *testing.T) {
		payload := []byte("s3-compatible small object")
		assertS3RoundTrip(t, ctx, store, uniqueIntegrationKey("small"), payload, int64(len(payload)), "text/plain", false)
	})

	t.Run("known multipart object", func(t *testing.T) {
		payload := bytes.Repeat([]byte("multipart-pattern-"), 1024*1024+1)
		assertS3RoundTrip(t, ctx, store, uniqueIntegrationKey("multipart"), payload, int64(len(payload)), "application/octet-stream", false)
	})

	t.Run("unknown length pipe object", func(t *testing.T) {
		payload := bytes.Repeat([]byte("streaming-pattern-"), 1024*1024+1)
		assertS3RoundTrip(t, ctx, store, uniqueIntegrationKey("stream"), payload, -1, "application/jsonl", true)
	})
}

func registerIntegrationBucketCleanup(t *testing.T, store ObjectStore) {
	t.Helper()
	if os.Getenv("OMA_S3_INTEGRATION_DELETE_BUCKET") != "1" {
		return
	}
	if !strings.HasPrefix(store.Name(), "oma-storage-test-") {
		t.Fatalf("refusing to delete non-temporary integration bucket %q", store.Name())
	}
	s3Store, ok := store.(*s3Store)
	if !ok {
		t.Fatalf("integration store type = %T, want *s3Store", store)
	}
	client, ok := s3Store.client.(interface {
		DeleteBucket(context.Context, *s3.DeleteBucketInput, ...func(*s3.Options)) (*s3.DeleteBucketOutput, error)
	})
	if !ok {
		t.Fatalf("integration client type = %T, want bucket deletion support", s3Store.client)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := client.DeleteBucket(cleanupCtx, &s3.DeleteBucketInput{Bucket: &s3Store.name}); err != nil {
			t.Errorf("DeleteBucket(%q) cleanup error = %v", s3Store.name, err)
			return
		}
		if _, err := s3Store.client.HeadBucket(cleanupCtx, &s3.HeadBucketInput{Bucket: &s3Store.name}); err == nil {
			t.Errorf("HeadBucket(%q) after cleanup error = nil, want not found", s3Store.name)
		}
	})
}

func assertS3RoundTrip(
	t *testing.T,
	ctx context.Context,
	store ObjectStore,
	key string,
	payload []byte,
	size int64,
	contentType string,
	usePipe bool,
) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := store.Delete(cleanupCtx, key, DeleteOptions{}); err != nil {
			t.Errorf("Delete(%q) cleanup error = %v", key, err)
		}
	})

	var uploadErr error
	var writerErr error
	if usePipe {
		reader, writer := io.Pipe()
		writerDone := make(chan error, 1)
		go func() {
			_, err := io.Copy(writer, bytes.NewReader(payload))
			if closeErr := writer.Close(); err == nil {
				err = closeErr
			}
			writerDone <- err
		}()
		_, uploadErr = store.Upload(ctx, key, reader, UploadOptions{Size: size, ContentType: contentType})
		_ = reader.CloseWithError(uploadErr)
		writerErr = <-writerDone
	} else {
		_, uploadErr = store.Upload(ctx, key, bytes.NewReader(payload), UploadOptions{Size: size, ContentType: contentType})
	}
	if uploadErr != nil {
		t.Fatalf("Upload(%q) error = %v", key, uploadErr)
	}
	if writerErr != nil {
		t.Fatalf("pipe writer error = %v", writerErr)
	}

	object, err := store.Open(ctx, key, nil)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", key, err)
	}
	defer object.Body.Close()
	body, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatalf("ReadAll(%q) error = %v", key, err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("Get(%q) body length = %d, want %d matching bytes", key, len(body), len(payload))
	}
	if object.Size != int64(len(payload)) {
		t.Fatalf("Get(%q) size = %d, want %d", key, object.Size, len(payload))
	}
	if object.ContentType != contentType {
		t.Fatalf("Get(%q) content type = %q, want %q", key, object.ContentType, contentType)
	}
	if err := store.Delete(ctx, key, DeleteOptions{}); err != nil {
		t.Fatalf("Delete(%q) error = %v", key, err)
	}
	deletedObject, err := store.Open(ctx, key, nil)
	if err == nil {
		_ = deletedObject.Body.Close()
		t.Fatalf("Get(%q) after delete error = nil, want not found", key)
	}
}

func uniqueIntegrationKey(kind string) string {
	return fmt.Sprintf("storage-integration/%s-%d", kind, time.Now().UnixNano())
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
