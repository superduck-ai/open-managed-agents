package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var (
	ErrBucketNameRequired   = errors.New("bucket name is required")
	ErrInvalidKey           = errors.New("invalid object key")
	ErrInvalidRange         = errors.New("invalid byte range")
	ErrInvalidDeleteOptions = errors.New("invalid delete options")
)

// ByteRange 表示从零开始的字节区间。Length 为 -1 时读取至对象末尾，
// 为正数时最多读取指定长度；nil 区间表示读取整个对象。
type ByteRange struct {
	Offset int64
	Length int64
}

// UploadOptions 描述上传对象的长度与内容类型。Size 为 -1 时表示流长度未知，
// 非负值会作为 Content-Length 交给 S3 上传器。
type UploadOptions struct {
	Size        int64
	ContentType string
}

// UploadResult 返回上传后由 S3 确认的对象标识和实际读取字节数。
type UploadResult struct {
	Size      int64
	ETag      string
	VersionID string
}

// CopyResult 返回服务端复制后生成的新对象版本。
type CopyResult struct {
	ETag      string
	VersionID string
}

// DeleteOptions 描述对象删除范围。VersionID 非空时只删除精确版本；
// AllVersions 为 true 时清除同一键的全部版本与删除标记；两者不能同时设置。
type DeleteOptions struct {
	VersionID   string
	AllVersions bool
}

// Upload 上传已知或未知长度的数据流，并返回对象的 ETag、版本及实际读取字节数。
func (b *s3Store) Upload(ctx context.Context, key string, body io.Reader, opts UploadOptions) (UploadResult, error) {
	if err := validateObjectKey(key); err != nil {
		return UploadResult{}, err
	}
	if body == nil {
		return UploadResult{}, fmt.Errorf("upload %q: body is required", key)
	}
	if opts.Size < -1 {
		return UploadResult{}, fmt.Errorf("upload %q: size must be -1 or non-negative", key)
	}
	result, err := b.uploadObject(ctx, key, body, opts.Size, opts.ContentType)
	if err != nil {
		return UploadResult{}, normalizeOperationError("upload", b.name, key, err)
	}
	return result, nil
}

func (b *s3Store) uploadObject(
	ctx context.Context,
	key string,
	body io.Reader,
	size int64,
	contentType string,
) (UploadResult, error) {
	counter := &countingReader{reader: body}
	input := &transfermanager.UploadObjectInput{
		Bucket: aws.String(b.name),
		Key:    aws.String(key),
		Body:   counter,
	}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	output, err := b.uploader.UploadObject(ctx, input)
	if err != nil {
		return UploadResult{}, err
	}
	if output == nil {
		return UploadResult{}, errors.New("S3 returned an empty upload response")
	}
	return UploadResult{
		Size:      counter.Size(),
		ETag:      normalizeETag(aws.ToString(output.ETag)),
		VersionID: aws.ToString(output.VersionID),
	}, nil
}

// Open 打开整个对象或指定字节区间，返回的 Body 必须由调用方关闭。
func (b *s3Store) Open(ctx context.Context, key string, byteRange *ByteRange) (Object, error) {
	if err := validateObjectKey(key); err != nil {
		return Object{}, err
	}
	rangeHeader, err := formatByteRange(byteRange)
	if err != nil {
		return Object{}, err
	}
	object, err := b.openObject(ctx, key, rangeHeader)
	if err != nil {
		return Object{}, normalizeOperationError("open", b.name, key, err)
	}
	return object, nil
}

func (b *s3Store) openObject(ctx context.Context, key, rangeHeader string) (Object, error) {
	input := &s3.GetObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)}
	if rangeHeader != "" {
		input.Range = aws.String(rangeHeader)
	}
	output, err := b.client.GetObject(ctx, input)
	if err != nil {
		return Object{}, err
	}
	if output == nil || output.Body == nil {
		return Object{}, errors.New("S3 returned an empty response body")
	}
	size := int64(-1)
	if output.ContentLength != nil {
		size = *output.ContentLength
	}
	return Object{
		Body:         output.Body,
		Size:         size,
		ContentType:  aws.ToString(output.ContentType),
		ContentRange: aws.ToString(output.ContentRange),
		ETag:         normalizeETag(aws.ToString(output.ETag)),
		VersionID:    aws.ToString(output.VersionId),
	}, nil
}

// Copy 让 S3 在服务端复制对象，避免应用服务器中转文件内容。
func (b *s3Store) Copy(ctx context.Context, sourceKey, destinationKey string) (CopyResult, error) {
	if err := validateObjectKey(sourceKey); err != nil {
		return CopyResult{}, fmt.Errorf("source: %w", err)
	}
	if err := validateObjectKey(destinationKey); err != nil {
		return CopyResult{}, fmt.Errorf("destination: %w", err)
	}
	output, err := b.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(b.name),
		Key:        aws.String(destinationKey),
		CopySource: aws.String(url.PathEscape(b.name + "/" + sourceKey)),
	})
	if err != nil {
		return CopyResult{}, normalizeOperationError("copy", b.name, destinationKey, err)
	}
	if output == nil {
		return CopyResult{}, errors.New("S3 returned an empty copy response")
	}
	result := CopyResult{VersionID: aws.ToString(output.VersionId)}
	if output.CopyObjectResult != nil {
		result.ETag = normalizeETag(aws.ToString(output.CopyObjectResult.ETag))
	}
	return result, nil
}

// bucketVersioningEnabled 返回桶是否已启用或暂停过版本控制。
// 暂停版本控制的桶仍可能保留历史版本，因此同样返回 true。
func (b *s3Store) bucketVersioningEnabled(ctx context.Context) (bool, error) {
	output, err := b.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(b.name)})
	if err != nil {
		return false, fmt.Errorf("get bucket versioning %q: %w", b.name, err)
	}
	if output == nil {
		return false, errors.New("S3 returned an empty bucket versioning response")
	}
	return output.Status != "", nil
}

// listObjectVersions 返回指定键的全部版本和删除标记；相同前缀的其他对象不会混入结果。
func (b *s3Store) listObjectVersions(ctx context.Context, key string) ([]string, error) {
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	paginator := s3.NewListObjectVersionsPaginator(b.client, &s3.ListObjectVersionsInput{
		Bucket: aws.String(b.name),
		Prefix: aws.String(key),
	})
	var versionIDs []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list object versions %q: %w", key, err)
		}
		for _, version := range page.Versions {
			if aws.ToString(version.Key) == key {
				versionIDs = append(versionIDs, listedVersionID(version.VersionId))
			}
		}
		for _, marker := range page.DeleteMarkers {
			if aws.ToString(marker.Key) == key {
				versionIDs = append(versionIDs, listedVersionID(marker.VersionId))
			}
		}
	}
	// S3 不保证版本与删除标记合并后的顺序；排序让清理行为和测试结果保持确定。
	sort.Strings(versionIDs)
	return versionIDs, nil
}

// Delete 按选项删除对象。默认执行普通 S3 删除；指定 VersionID 时删除精确版本；
// AllVersions 会清除该键的全部版本和删除标记，防止留下不可达对象。
func (b *s3Store) Delete(ctx context.Context, key string, opts DeleteOptions) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}
	if opts.VersionID != "" && opts.AllVersions {
		return fmt.Errorf("%w: version ID and all versions are mutually exclusive", ErrInvalidDeleteOptions)
	}
	if opts.VersionID != "" {
		return b.deleteVersion(ctx, key, opts.VersionID)
	}
	if !opts.AllVersions {
		return b.deleteVersion(ctx, key, "")
	}

	versioningEnabled, err := b.bucketVersioningEnabled(ctx)
	if err != nil {
		return normalizeOperationError("get bucket versioning", b.name, key, err)
	}
	if !versioningEnabled {
		return b.deleteVersion(ctx, key, "")
	}
	return b.deleteAllVersions(ctx, key)
}

func (b *s3Store) deleteAllVersions(ctx context.Context, key string) error {
	versionIDs, err := b.listObjectVersions(ctx, key)
	if err != nil {
		return normalizeOperationError("list object versions", b.name, key, err)
	}

	var errs []error
	for _, versionID := range versionIDs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := b.deleteVersion(ctx, key, versionID); err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, err)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
		}
	}
	return errors.Join(errs...)
}

func (b *s3Store) deleteVersion(ctx context.Context, key, versionID string) error {
	input := &s3.DeleteObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	_, err := b.client.DeleteObject(ctx, input)
	return normalizeOperationError("delete", b.name, key, err)
}

func validateObjectKey(key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	return nil
}

func formatByteRange(byteRange *ByteRange) (string, error) {
	if byteRange == nil {
		return "", nil
	}
	if byteRange.Offset < 0 {
		return "", fmt.Errorf("%w: offset must not be negative", ErrInvalidRange)
	}
	if byteRange.Length == -1 {
		return fmt.Sprintf("bytes=%d-", byteRange.Offset), nil
	}
	if byteRange.Length <= 0 {
		return "", fmt.Errorf("%w: length must be positive or -1", ErrInvalidRange)
	}
	if byteRange.Offset > math.MaxInt64-(byteRange.Length-1) {
		return "", fmt.Errorf("%w: offset plus length overflows", ErrInvalidRange)
	}
	return fmt.Sprintf("bytes=%d-%d", byteRange.Offset, byteRange.Offset+byteRange.Length-1), nil
}

func normalizeETag(etag string) string {
	return strings.Trim(etag, "\"")
}

func listedVersionID(versionID *string) string {
	if value := aws.ToString(versionID); value != "" {
		return value
	}
	// 版本控制暂停后，S3 用字面量 null 标识空版本；删除时必须原样传回。
	return "null"
}

type countingReader struct {
	reader io.Reader
	size   atomic.Int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.size.Add(int64(n))
	return n, err
}

func (r *countingReader) Size() int64 {
	return r.size.Load()
}
