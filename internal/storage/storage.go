package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Object struct {
	Body        io.ReadCloser
	Size        int64
	ContentType string
}

type ObjectStore interface {
	EnsureBucket(ctx context.Context) error
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (Object, error)
	Delete(ctx context.Context, key string) error
	Bucket() string
}

type MinIOStore struct {
	client *minio.Client
	core   *minio.Core
	bucket string
	region string
}

func New(cfg config.StorageConfig) (ObjectStore, error) {
	switch cfg.Type {
	case config.StorageTypeS3:
		return newS3(cfg.S3)
	default:
		return nil, fmt.Errorf("unsupported object storage type %q", cfg.Type)
	}
}

func newS3(cfg config.S3Config) (*MinIOStore, error) {
	endpoint, secure, err := normalizeEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	bucketLookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		bucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: bucketLookup,
	})
	if err != nil {
		return nil, err
	}
	return &MinIOStore{client: client, core: &minio.Core{Client: client}, bucket: cfg.Bucket, region: cfg.Region}, nil
}

func (s *MinIOStore) Bucket() string {
	return s.bucket
}

func (s *MinIOStore) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %q: %w", s.bucket, err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
		return fmt.Errorf("create bucket %q: %w", s.bucket, err)
	}
	return nil
}

func (s *MinIOStore) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

func (s *MinIOStore) Get(ctx context.Context, key string) (Object, error) {
	body, info, _, err := s.core.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return Object{}, fmt.Errorf("get object %q: %w", key, err)
	}
	return Object{Body: body, Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}

func normalizeEndpoint(raw string) (endpoint string, secure bool, err error) {
	if !strings.Contains(raw, "://") {
		return raw, false, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parse storage.s3.endpoint: %w", err)
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("storage.s3.endpoint %q is missing host", raw)
	}
	switch parsed.Scheme {
	case "http":
		return parsed.Host, false, nil
	case "https":
		return parsed.Host, true, nil
	default:
		return "", false, fmt.Errorf("storage.s3.endpoint scheme %q is unsupported", parsed.Scheme)
	}
}
