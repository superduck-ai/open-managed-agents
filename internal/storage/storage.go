package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const (
	multipartPartSizeBytes  int64 = 16 * 1024 * 1024
	multipartAbortTimeout         = 10 * time.Second
	s3ResponseHeaderTimeout       = 30 * time.Second
)

// Object is a streamed S3 object. Size is -1 when the response omits Content-Length.
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

type s3API interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type objectUploader interface {
	UploadObject(context.Context, *transfermanager.UploadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

type S3Store struct {
	client   s3API
	uploader objectUploader
	bucket   string
	region   string
}

func New(cfg config.StorageConfig) (ObjectStore, error) {
	switch cfg.Type {
	case config.StorageTypeS3:
		return newS3(cfg.S3)
	default:
		return nil, fmt.Errorf("unsupported object storage type %q", cfg.Type)
	}
}

func newS3(cfg config.S3Config) (*S3Store, error) {
	endpoint, err := normalizeEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	awsConfig := newAWSConfig(cfg)
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		configureS3Options(options, endpoint, cfg.ForcePathStyle)
	})
	uploader := transfermanager.New(client, configureTransferOptions)
	return &S3Store{client: client, uploader: uploader, bucket: cfg.Bucket, region: cfg.Region}, nil
}

func newAWSConfig(cfg config.S3Config) aws.Config {
	return aws.Config{
		Region:     cfg.Region,
		HTTPClient: newS3HTTPClient(),
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
	}
}

func newS3HTTPClient() aws.HTTPClient {
	return awshttp.NewBuildableClient().WithTransportOptions(func(transport *http.Transport) {
		transport.ResponseHeaderTimeout = s3ResponseHeaderTimeout
	})
}

func configureS3Options(options *s3.Options, endpoint string, forcePathStyle bool) {
	options.BaseEndpoint = aws.String(endpoint)
	options.UsePathStyle = forcePathStyle
	options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	options.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
}

func configureTransferOptions(options *transfermanager.Options) {
	options.PartSizeBytes = multipartPartSizeBytes
	options.MultipartUploadThreshold = multipartPartSizeBytes
	// Keep one part in flight so io.Pipe uploads retain backpressure and bounded memory use.
	options.Concurrency = 1
	options.FailTimeout = multipartAbortTimeout
	options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
}

func (s *S3Store) Bucket() string {
	return s.bucket
}

func (s *S3Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	if !isBucketMissing(err) {
		return fmt.Errorf("check bucket %q: %w", s.bucket, err)
	}

	input := &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}
	if s.region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.region),
		}
	}
	if _, err := s.client.CreateBucket(ctx, input); err != nil {
		if !isBucketCreateConflict(err) {
			return fmt.Errorf("create bucket %q: %w", s.bucket, err)
		}
		if _, recheckErr := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}); recheckErr != nil {
			return fmt.Errorf("create bucket %q returned a conflict (%w); recheck failed: %w", s.bucket, err, recheckErr)
		}
	}
	return nil
}

func (s *S3Store) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	input := &transfermanager.UploadObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	if _, err := s.uploader.UploadObject(ctx, input); err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

func (s *S3Store) Get(ctx context.Context, key string) (Object, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return Object{}, fmt.Errorf("get object %q: %w", key, err)
	}
	if output == nil || output.Body == nil {
		return Object{}, fmt.Errorf("get object %q: S3 returned an empty response body", key)
	}
	size := int64(-1)
	if output.ContentLength != nil {
		size = *output.ContentLength
	}
	return Object{
		Body:        output.Body,
		Size:        size,
		ContentType: aws.ToString(output.ContentType),
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}

func isBucketMissing(err error) bool {
	if status, ok := httpStatusCode(err); ok {
		return status == http.StatusNotFound
	}
	return hasAPIErrorCode(err, "NotFound", "NoSuchBucket")
}

func isBucketCreateConflict(err error) bool {
	if status, ok := httpStatusCode(err); ok {
		return status == http.StatusConflict
	}
	return hasAPIErrorCode(err, "BucketAlreadyExists", "BucketAlreadyOwnedByYou")
}

func hasAPIErrorCode(err error, codes ...string) bool {
	var apiError interface {
		ErrorCode() string
	}
	if !errors.As(err, &apiError) {
		return false
	}
	for _, code := range codes {
		if apiError.ErrorCode() == code {
			return true
		}
	}
	return false
}

func httpStatusCode(err error) (int, bool) {
	var statusError interface {
		HTTPStatusCode() int
	}
	if !errors.As(err, &statusError) {
		return 0, false
	}
	return statusError.HTTPStatusCode(), true
}

func normalizeEndpoint(raw string) (string, error) {
	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("parse storage.s3.endpoint: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("storage.s3.endpoint %q is missing host", raw)
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed.Scheme + "://" + parsed.Host, nil
	default:
		return "", fmt.Errorf("storage.s3.endpoint scheme %q is unsupported", parsed.Scheme)
	}
}
