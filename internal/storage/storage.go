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
	Body         io.ReadCloser
	Size         int64
	ContentType  string
	ContentRange string
	ETag         string
	VersionID    string
}

// Client 复用同一套对象存储连接与凭证，并按名称派生绑定单个 bucket 的存储边界。
type Client interface {
	ForBucket(name string) (ObjectStore, error)
}

// ObjectStore 表示绑定到单个 bucket 的对象操作边界。
type ObjectStore interface {
	Ensure(ctx context.Context) error
	Name() string
	Upload(ctx context.Context, key string, body io.Reader, opts UploadOptions) (UploadResult, error)
	Open(ctx context.Context, key string, byteRange *ByteRange) (Object, error)
	Copy(ctx context.Context, sourceKey, destinationKey string) (CopyResult, error)
	Delete(ctx context.Context, key string, opts DeleteOptions) error
}

// s3API 只保留适配器实际使用的 AWS S3 Client 能力，便于在单元测试中替换底层 SDK。
type s3API interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	GetBucketVersioning(context.Context, *s3.GetBucketVersioningInput, ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error)
	ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
}

// transferManagerAPI 只描述流式传输所需能力，不把完整 Transfer Manager 暴露给 ObjectStore。
type transferManagerAPI interface {
	GetObject(ctx context.Context, input *transfermanager.GetObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.GetObjectOutput, error)
	UploadObject(context.Context, *transfermanager.UploadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

// S3Client 复用一套 AWS SDK 客户端，为多个 ObjectStore 提供服务。
type S3Client struct {
	client   s3API
	uploader transferManagerAPI
	region   string
}

// s3Store 保存不可变的 bucket 名称，并共享 S3Client 已初始化的 SDK 依赖。
type s3Store struct {
	client   s3API
	uploader transferManagerAPI
	name     string
	region   string
}

var (
	_ Client      = (*S3Client)(nil)
	_ ObjectStore = (*s3Store)(nil)
)

// New 根据存储配置创建可复用于多个 bucket 的客户端。
func New(cfg config.StorageConfig) (Client, error) {
	switch cfg.Type {
	case config.StorageTypeS3:
		return newS3(cfg.S3)
	default:
		return nil, fmt.Errorf("unsupported object storage type %q", cfg.Type)
	}
}

func newS3(cfg config.S3Config) (*S3Client, error) {
	endpoint, err := normalizeEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	awsConfig := newAWSConfig(cfg)
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		configureS3Options(options, endpoint, cfg.ForcePathStyle)
	})
	uploader := transfermanager.New(client, configureTransferOptions)
	return &S3Client{client: client, uploader: uploader, region: cfg.Region}, nil
}

// ForBucket 返回绑定到指定 bucket 的对象存储，不执行网络请求或创建远端 bucket。
func (c *S3Client) ForBucket(name string) (ObjectStore, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrBucketNameRequired
	}
	return &s3Store{
		client:   c.client,
		uploader: c.uploader,
		name:     name,
		region:   c.region,
	}, nil
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

func (b *s3Store) Name() string {
	return b.name
}

func (b *s3Store) Ensure(ctx context.Context) error {
	_, err := b.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(b.name)})
	if err == nil {
		return nil
	}
	if !isBucketMissing(err) {
		return fmt.Errorf("check bucket %q: %w", b.name, err)
	}

	input := &s3.CreateBucketInput{Bucket: aws.String(b.name)}
	if b.region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(b.region),
		}
	}
	if _, err := b.client.CreateBucket(ctx, input); err != nil {
		if !isBucketCreateConflict(err) {
			return fmt.Errorf("create bucket %q: %w", b.name, err)
		}
		if _, recheckErr := b.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(b.name)}); recheckErr != nil {
			return fmt.Errorf("create bucket %q returned a conflict (%w); recheck failed: %w", b.name, err, recheckErr)
		}
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
	status := statusError.HTTPStatusCode()
	if status <= 0 {
		return 0, false
	}
	return status, true
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
	if parsed.User != nil {
		return "", errors.New("storage.s3.endpoint must not include user information")
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed.Scheme + "://" + parsed.Host, nil
	default:
		return "", fmt.Errorf("storage.s3.endpoint scheme %q is unsupported", parsed.Scheme)
	}
}
