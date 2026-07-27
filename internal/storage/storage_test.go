package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestNewClient(t *testing.T) {
	t.Run("failure unsupported type", func(t *testing.T) {
		_, err := New(config.StorageConfig{Type: "filesystem"})
		if err == nil || !strings.Contains(err.Error(), "unsupported object storage type") {
			t.Fatalf("New() error = %v, want unsupported type error", err)
		}
	})

	t.Run("failure blank bucket name", func(t *testing.T) {
		client := &S3Client{}
		if _, err := client.ForBucket(" \t"); !errors.Is(err, ErrBucketNameRequired) {
			t.Fatalf("ForBucket(blank) error = %v, want %v", err, ErrBucketNameRequired)
		}
	})

	t.Run("success s3", func(t *testing.T) {
		client, err := New(config.StorageConfig{
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
		if _, ok := client.(*S3Client); !ok {
			t.Fatalf("New() type = %T, want *S3Client", client)
		}

		first, err := client.ForBucket("first-bucket")
		if err != nil {
			t.Fatalf("ForBucket(first-bucket) error = %v", err)
		}
		second, err := client.ForBucket("second-bucket")
		if err != nil {
			t.Fatalf("ForBucket(second-bucket) error = %v", err)
		}
		if first.Name() != "first-bucket" || second.Name() != "second-bucket" {
			t.Fatalf("bucket names = %q/%q, want first-bucket/second-bucket", first.Name(), second.Name())
		}
		firstS3 := first.(*s3Store)
		secondS3 := second.(*s3Store)
		if firstS3.client != secondS3.client || firstS3.uploader != secondS3.uploader {
			t.Fatal("bucket handles do not share the client's AWS SDK dependencies")
		}
	})
}

func TestS3ClientObjectStoresRouteOperationsIndependently(t *testing.T) {
	var deletedBuckets []string
	client := &S3Client{client: &fakeS3API{
		deleteObject: func(_ context.Context, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
			deletedBuckets = append(deletedBuckets, aws.ToString(input.Bucket))
			return &s3.DeleteObjectOutput{}, nil
		},
	}}
	first, err := client.ForBucket("first-bucket")
	if err != nil {
		t.Fatalf("ForBucket(first-bucket) error = %v", err)
	}
	second, err := client.ForBucket("second-bucket")
	if err != nil {
		t.Fatalf("ForBucket(second-bucket) error = %v", err)
	}
	if err := first.Delete(context.Background(), "first-key", DeleteOptions{}); err != nil {
		t.Fatalf("first Delete() error = %v", err)
	}
	if err := second.Delete(context.Background(), "second-key", DeleteOptions{}); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if !slices.Equal(deletedBuckets, []string{"first-bucket", "second-bucket"}) {
		t.Fatalf("DeleteObject buckets = %v, want first-bucket/second-bucket", deletedBuckets)
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "failure malformed", raw: "://bad", wantErr: "parse storage.s3.endpoint"},
		{name: "failure missing host", raw: "http:///prefix", wantErr: "missing host"},
		{name: "failure unsupported scheme", raw: "ftp://localhost:9000", wantErr: "scheme \"ftp\" is unsupported"},
		{name: "failure user information", raw: "http://user:pass@localhost:9000", wantErr: "must not include user information"},
		{name: "success defaults to http", raw: "localhost:9000", want: "http://localhost:9000"},
		{name: "success strips path without scheme", raw: "localhost:9000/prefix", want: "http://localhost:9000"},
		{name: "success preserves https host", raw: "https://objects.example.com:9443/prefix?ignored=true", want: "https://objects.example.com:9443"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeEndpoint(test.raw)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("normalizeEndpoint() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEndpoint() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("normalizeEndpoint() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestS3ClientConfiguration(t *testing.T) {
	cfg := config.S3Config{
		Region:          "test-region-1",
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
	}
	awsConfig := newAWSConfig(cfg)
	if awsConfig.Region != cfg.Region {
		t.Fatalf("Region = %q, want %q", awsConfig.Region, cfg.Region)
	}
	httpClient, ok := awsConfig.HTTPClient.(interface {
		GetTransport() *http.Transport
	})
	if !ok {
		t.Fatalf("HTTPClient type = %T, want buildable transport client", awsConfig.HTTPClient)
	}
	if got := httpClient.GetTransport().ResponseHeaderTimeout; got != s3ResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", got, s3ResponseHeaderTimeout)
	}
	credentials, err := awsConfig.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if credentials.AccessKeyID != cfg.AccessKeyID || credentials.SecretAccessKey != cfg.SecretAccessKey {
		t.Fatalf("credentials = %q/%q, want configured static credentials", credentials.AccessKeyID, credentials.SecretAccessKey)
	}

	var options s3.Options
	configureS3Options(&options, "https://objects.example.com", true)
	if aws.ToString(options.BaseEndpoint) != "https://objects.example.com" {
		t.Fatalf("BaseEndpoint = %q", aws.ToString(options.BaseEndpoint))
	}
	if !options.UsePathStyle {
		t.Fatal("UsePathStyle = false, want true")
	}
	if options.RequestChecksumCalculation != aws.RequestChecksumCalculationWhenRequired {
		t.Fatalf("RequestChecksumCalculation = %v", options.RequestChecksumCalculation)
	}
	if options.ResponseChecksumValidation != aws.ResponseChecksumValidationWhenRequired {
		t.Fatalf("ResponseChecksumValidation = %v", options.ResponseChecksumValidation)
	}
}

func TestTransferConfiguration(t *testing.T) {
	var options transfermanager.Options
	configureTransferOptions(&options)
	if options.PartSizeBytes != multipartPartSizeBytes {
		t.Fatalf("PartSizeBytes = %d, want %d", options.PartSizeBytes, multipartPartSizeBytes)
	}
	if options.MultipartUploadThreshold != multipartPartSizeBytes {
		t.Fatalf("MultipartUploadThreshold = %d, want %d", options.MultipartUploadThreshold, multipartPartSizeBytes)
	}
	if options.Concurrency != 1 {
		t.Fatalf("Concurrency = %d, want 1", options.Concurrency)
	}
	if options.FailTimeout != multipartAbortTimeout {
		t.Fatalf("FailTimeout = %s, want %s", options.FailTimeout, multipartAbortTimeout)
	}
	if options.RequestChecksumCalculation != aws.RequestChecksumCalculationWhenRequired {
		t.Fatalf("RequestChecksumCalculation = %v", options.RequestChecksumCalculation)
	}
}

func TestS3BucketEnsure(t *testing.T) {
	t.Run("failure head forbidden does not create", func(t *testing.T) {
		createCalls := 0
		client := &fakeS3API{
			headBucket: func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, fmt.Errorf("wrapped: %w", testS3Error{status: http.StatusForbidden, code: "NoSuchBucket"})
			},
			createBucket: func(context.Context, *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
				createCalls++
				return &s3.CreateBucketOutput{}, nil
			},
		}
		store := testBucket(client, nil, "us-east-1")
		err := store.Ensure(context.Background())
		if err == nil || !strings.Contains(err.Error(), "check bucket") {
			t.Fatalf("Ensure() error = %v, want check error", err)
		}
		if createCalls != 0 {
			t.Fatalf("CreateBucket calls = %d, want 0", createCalls)
		}
	})

	t.Run("failure head server error does not create", func(t *testing.T) {
		createCalls := 0
		client := &fakeS3API{
			headBucket: func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, testHTTPStatusError(http.StatusInternalServerError)
			},
			createBucket: func(context.Context, *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
				createCalls++
				return &s3.CreateBucketOutput{}, nil
			},
		}
		err := testBucket(client, nil, "us-east-1").Ensure(context.Background())
		if err == nil || !strings.Contains(err.Error(), "check bucket") {
			t.Fatalf("Ensure() error = %v, want check error", err)
		}
		if createCalls != 0 {
			t.Fatalf("CreateBucket calls = %d, want 0", createCalls)
		}
	})

	t.Run("failure create conflict remains inaccessible", func(t *testing.T) {
		headCalls := 0
		createErr := testHTTPStatusError(http.StatusConflict)
		recheckErr := testHTTPStatusError(http.StatusForbidden)
		client := &fakeS3API{
			headBucket: func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				headCalls++
				if headCalls == 1 {
					return nil, testHTTPStatusError(http.StatusNotFound)
				}
				return nil, recheckErr
			},
			createBucket: func(context.Context, *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
				return nil, createErr
			},
		}
		err := testBucket(client, nil, "us-east-1").Ensure(context.Background())
		if !errors.Is(err, createErr) || !errors.Is(err, recheckErr) {
			t.Fatalf("Ensure() error = %v, want wrapped create and recheck errors", err)
		}
		if headCalls != 2 {
			t.Fatalf("HeadBucket calls = %d, want 2", headCalls)
		}
	})

	t.Run("failure create after not found", func(t *testing.T) {
		createErr := errors.New("create failed")
		client := &fakeS3API{
			headBucket: func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, testHTTPStatusError(http.StatusNotFound)
			},
			createBucket: func(context.Context, *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
				return nil, createErr
			},
		}
		err := testBucket(client, nil, "us-east-1").Ensure(context.Background())
		if !errors.Is(err, createErr) || !strings.Contains(err.Error(), "create bucket") {
			t.Fatalf("Ensure() error = %v, want wrapped create error", err)
		}
	})

	t.Run("success missing API code when HTTP status is unavailable", func(t *testing.T) {
		createCalls := 0
		client := &fakeS3API{
			headBucket: func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, testS3Error{status: 0, code: "NoSuchBucket"}
			},
			createBucket: func(context.Context, *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
				createCalls++
				return &s3.CreateBucketOutput{}, nil
			},
		}
		if err := testBucket(client, nil, "us-east-1").Ensure(context.Background()); err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
		if createCalls != 1 {
			t.Fatalf("CreateBucket calls = %d, want 1", createCalls)
		}
	})

	t.Run("success existing", func(t *testing.T) {
		client := &fakeS3API{
			headBucket: func(_ context.Context, input *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				if aws.ToString(input.Bucket) != "test-bucket" {
					t.Fatalf("HeadBucket bucket = %q", aws.ToString(input.Bucket))
				}
				return &s3.HeadBucketOutput{}, nil
			},
		}
		if err := testBucket(client, nil, "us-east-1").Ensure(context.Background()); err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
	})

	t.Run("success concurrent creator wins race", func(t *testing.T) {
		headCalls := 0
		client := &fakeS3API{
			headBucket: func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				headCalls++
				if headCalls == 1 {
					return nil, &types.NoSuchBucket{Message: aws.String("missing")}
				}
				return &s3.HeadBucketOutput{}, nil
			},
			createBucket: func(context.Context, *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
				return nil, testS3Error{status: 0, code: "BucketAlreadyOwnedByYou"}
			},
		}
		if err := testBucket(client, nil, "us-east-1").Ensure(context.Background()); err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
		if headCalls != 2 {
			t.Fatalf("HeadBucket calls = %d, want 2", headCalls)
		}
	})

	t.Run("success create us east omits location", func(t *testing.T) {
		client := notFoundThenCreateClient(t, func(input *s3.CreateBucketInput) {
			if input.CreateBucketConfiguration != nil {
				t.Fatalf("CreateBucketConfiguration = %#v, want nil", input.CreateBucketConfiguration)
			}
		})
		if err := testBucket(client, nil, "us-east-1").Ensure(context.Background()); err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
	})

	t.Run("success create configured region", func(t *testing.T) {
		client := notFoundThenCreateClient(t, func(input *s3.CreateBucketInput) {
			if input.CreateBucketConfiguration == nil {
				t.Fatal("CreateBucketConfiguration = nil")
			}
			if got := input.CreateBucketConfiguration.LocationConstraint; got != types.BucketLocationConstraintEuWest1 {
				t.Fatalf("LocationConstraint = %q, want %q", got, types.BucketLocationConstraintEuWest1)
			}
		})
		if err := testBucket(client, nil, "eu-west-1").Ensure(context.Background()); err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
	})
}

func TestS3BucketUploadOptionsAndStreaming(t *testing.T) {
	t.Run("failure upload", func(t *testing.T) {
		uploadErr := errors.New("upload failed")
		uploader := &fakeUploader{upload: func(context.Context, *transfermanager.UploadObjectInput) (*transfermanager.UploadObjectOutput, error) {
			return nil, uploadErr
		}}
		_, err := testBucket(nil, uploader, "us-east-1").Upload(context.Background(), "failed-key", strings.NewReader("body"), UploadOptions{Size: 4, ContentType: "text/plain"})
		if !errors.Is(err, uploadErr) || !strings.Contains(err.Error(), "upload") {
			t.Fatalf("Upload() error = %v, want wrapped upload error", err)
		}
	})

	t.Run("failure multipart aborts upload", func(t *testing.T) {
		uploadErr := errors.New("part upload failed")
		client := &failingMultipartS3API{uploadErr: uploadErr}
		uploader := transfermanager.New(client, configureTransferOptions)
		payload := bytes.Repeat([]byte("x"), int(multipartPartSizeBytes+1))
		_, err := testBucket(nil, uploader, "us-east-1").Upload(
			context.Background(),
			"multipart-key",
			bytes.NewReader(payload),
			UploadOptions{Size: int64(len(payload)), ContentType: "application/octet-stream"},
		)
		if !errors.Is(err, uploadErr) {
			t.Fatalf("Upload() error = %v, want wrapped part upload error", err)
		}
		if client.abortCalls != 1 {
			t.Fatalf("AbortMultipartUpload calls = %d, want 1", client.abortCalls)
		}
	})

	t.Run("success known size", func(t *testing.T) {
		var got *transfermanager.UploadObjectInput
		uploader := &fakeUploader{upload: func(_ context.Context, input *transfermanager.UploadObjectInput) (*transfermanager.UploadObjectOutput, error) {
			got = input
			return &transfermanager.UploadObjectOutput{}, nil
		}}
		if _, err := testBucket(nil, uploader, "us-east-1").Upload(context.Background(), "known-key", strings.NewReader("body"), UploadOptions{Size: 4, ContentType: "text/plain"}); err != nil {
			t.Fatalf("Upload() error = %v", err)
		}
		assertUploadInput(t, got, "known-key", "text/plain")
		if got.ContentLength == nil || aws.ToInt64(got.ContentLength) != 4 {
			t.Fatalf("ContentLength = %v, want 4", got.ContentLength)
		}
	})

	t.Run("success omits empty content type", func(t *testing.T) {
		var got *transfermanager.UploadObjectInput
		uploader := &fakeUploader{upload: func(_ context.Context, input *transfermanager.UploadObjectInput) (*transfermanager.UploadObjectOutput, error) {
			got = input
			return &transfermanager.UploadObjectOutput{}, nil
		}}
		if _, err := testBucket(nil, uploader, "us-east-1").Upload(context.Background(), "empty-content-type", strings.NewReader("body"), UploadOptions{Size: 4}); err != nil {
			t.Fatalf("Upload() error = %v", err)
		}
		if got == nil {
			t.Fatal("UploadObject input = nil")
		}
		if got.ContentType != nil {
			t.Fatalf("ContentType = %q, want nil", aws.ToString(got.ContentType))
		}
	})

	t.Run("success unknown size", func(t *testing.T) {
		var got *transfermanager.UploadObjectInput
		uploader := &fakeUploader{upload: func(_ context.Context, input *transfermanager.UploadObjectInput) (*transfermanager.UploadObjectOutput, error) {
			got = input
			return &transfermanager.UploadObjectOutput{}, nil
		}}
		if _, err := testBucket(nil, uploader, "us-east-1").Upload(context.Background(), "stream-key", strings.NewReader("body"), UploadOptions{Size: -1, ContentType: "application/jsonl"}); err != nil {
			t.Fatalf("Upload() error = %v", err)
		}
		assertUploadInput(t, got, "stream-key", "application/jsonl")
		if got.ContentLength != nil {
			t.Fatalf("ContentLength = %v, want nil", got.ContentLength)
		}
	})

	t.Run("success unknown size non-seekable multipart", func(t *testing.T) {
		client := &recordingMultipartS3API{}
		uploader := transfermanager.New(client, configureTransferOptions)
		payload := bytes.Repeat([]byte("x"), int(multipartPartSizeBytes+1))
		reader, writer := io.Pipe()
		writeDone := make(chan error, 1)
		go func() {
			_, writeErr := io.Copy(writer, bytes.NewReader(payload))
			if closeErr := writer.Close(); writeErr == nil {
				writeErr = closeErr
			}
			writeDone <- writeErr
		}()

		_, uploadErr := testBucket(nil, uploader, "us-east-1").Upload(
			context.Background(),
			"stream-multipart-key",
			reader,
			UploadOptions{Size: -1, ContentType: "application/octet-stream"},
		)
		if uploadErr != nil {
			_ = reader.CloseWithError(uploadErr)
			t.Fatalf("Upload() error = %v", uploadErr)
		}
		if writeErr := <-writeDone; writeErr != nil {
			t.Fatalf("pipe writer error = %v", writeErr)
		}

		createCalls, uploadCalls, completeCalls, abortCalls, uploadedBytes, completedParts := client.snapshot()
		if createCalls != 1 || completeCalls != 1 || abortCalls != 0 {
			t.Fatalf("multipart calls = create %d complete %d abort %d, want 1/1/0", createCalls, completeCalls, abortCalls)
		}
		if uploadCalls < 2 || completedParts != uploadCalls {
			t.Fatalf("multipart parts = uploaded %d completed %d, want at least two matching parts", uploadCalls, completedParts)
		}
		if uploadedBytes != int64(len(payload)) {
			t.Fatalf("uploaded bytes = %d, want %d", uploadedBytes, len(payload))
		}
	})
}

func TestS3BucketOpenWholeObject(t *testing.T) {
	t.Run("failure get", func(t *testing.T) {
		getErr := errors.New("get failed")
		client := &fakeS3API{getObject: func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return nil, getErr
		}}
		_, err := testBucket(client, nil, "us-east-1").Open(context.Background(), "failed-key", nil)
		if !errors.Is(err, getErr) || !strings.Contains(err.Error(), "open") {
			t.Fatalf("Open() error = %v, want wrapped get error", err)
		}
	})

	t.Run("failure nil output", func(t *testing.T) {
		client := &fakeS3API{getObject: func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return nil, nil
		}}
		_, err := testBucket(client, nil, "us-east-1").Open(context.Background(), "empty-key", nil)
		if err == nil || !strings.Contains(err.Error(), "empty response body") {
			t.Fatalf("Open() error = %v, want empty response body error", err)
		}
	})

	t.Run("failure nil body", func(t *testing.T) {
		client := &fakeS3API{getObject: func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{}, nil
		}}
		_, err := testBucket(client, nil, "us-east-1").Open(context.Background(), "empty-key", nil)
		if err == nil || !strings.Contains(err.Error(), "empty response body") {
			t.Fatalf("Open() error = %v, want empty response body error", err)
		}
	})

	t.Run("success preserves unknown size", func(t *testing.T) {
		client := &fakeS3API{getObject: func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("body"))}, nil
		}}
		object, err := testBucket(client, nil, "us-east-1").Open(context.Background(), "unknown-size-key", nil)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer object.Body.Close()
		if object.Size != -1 {
			t.Fatalf("Object.Size = %d, want -1", object.Size)
		}
	})

	t.Run("success maps object", func(t *testing.T) {
		client := &fakeS3API{getObject: func(_ context.Context, input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			if aws.ToString(input.Bucket) != "test-bucket" || aws.ToString(input.Key) != "object-key" {
				t.Fatalf("GetObject input = bucket %q key %q", aws.ToString(input.Bucket), aws.ToString(input.Key))
			}
			return &s3.GetObjectOutput{
				Body:          io.NopCloser(strings.NewReader("object body")),
				ContentLength: aws.Int64(11),
				ContentType:   aws.String("text/plain"),
			}, nil
		}}
		object, err := testBucket(client, nil, "us-east-1").Open(context.Background(), "object-key", nil)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer object.Body.Close()
		body, err := io.ReadAll(object.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(body) != "object body" || object.Size != 11 || object.ContentType != "text/plain" {
			t.Fatalf("object = body %q size %d content type %q", body, object.Size, object.ContentType)
		}
	})
}

func TestS3BucketDelete(t *testing.T) {
	t.Run("failure delete", func(t *testing.T) {
		deleteErr := errors.New("delete failed")
		client := &fakeS3API{deleteObject: func(context.Context, *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
			return nil, deleteErr
		}}
		err := testBucket(client, nil, "us-east-1").Delete(context.Background(), "failed-key", DeleteOptions{})
		if !errors.Is(err, deleteErr) || !strings.Contains(err.Error(), "object storage delete") {
			t.Fatalf("Delete() error = %v, want wrapped delete error", err)
		}
	})

	t.Run("success delete", func(t *testing.T) {
		client := &fakeS3API{deleteObject: func(_ context.Context, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
			if aws.ToString(input.Bucket) != "test-bucket" || aws.ToString(input.Key) != "object-key" {
				t.Fatalf("DeleteObject input = bucket %q key %q", aws.ToString(input.Bucket), aws.ToString(input.Key))
			}
			return &s3.DeleteObjectOutput{}, nil
		}}
		if err := testBucket(client, nil, "us-east-1").Delete(context.Background(), "object-key", DeleteOptions{}); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
	})
}

func TestS3BucketUploadReturnsObjectIdentityAndActualSize(t *testing.T) {
	t.Parallel()

	uploader := &fakeUploader{upload: func(_ context.Context, input *transfermanager.UploadObjectInput) (*transfermanager.UploadObjectOutput, error) {
		contents, err := io.ReadAll(input.Body)
		if err != nil {
			return nil, err
		}
		if got := string(contents); got != "stream-without-known-size" {
			t.Fatalf("UploadObject body = %q", got)
		}
		if aws.ToString(input.Key) != "objects/file" || aws.ToString(input.ContentType) != "text/plain" {
			t.Fatalf("UploadObject input = key %q content type %q", aws.ToString(input.Key), aws.ToString(input.ContentType))
		}
		return &transfermanager.UploadObjectOutput{
			ETag:      aws.String("\"upload-etag\""),
			VersionID: aws.String("version-2"),
		}, nil
	}}
	store := testBucket(&fakeS3API{}, uploader, "us-east-1")

	result, err := store.Upload(
		context.Background(),
		"objects/file",
		strings.NewReader("stream-without-known-size"),
		UploadOptions{Size: -1, ContentType: "text/plain"},
	)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Size != int64(len("stream-without-known-size")) || result.ETag != "upload-etag" || result.VersionID != "version-2" {
		t.Fatalf("Upload() result = %+v", result)
	}
}

func TestS3BucketOpenFormatsRangeAndReturnsMetadata(t *testing.T) {
	t.Parallel()

	var captured *s3.GetObjectInput
	client := &fakeS3API{getObject: func(_ context.Context, input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
		captured = input
		return &s3.GetObjectOutput{
			Body:          io.NopCloser(strings.NewReader("partial")),
			ContentLength: aws.Int64(7),
			ContentType:   aws.String("text/plain"),
			ContentRange:  aws.String("bytes 2-8/10"),
			ETag:          aws.String("\"etag\""),
			VersionId:     aws.String("version"),
		}, nil
	}}
	store := testBucket(client, nil, "us-east-1")

	object, err := store.Open(context.Background(), "objects/file", &ByteRange{Offset: 2, Length: 7})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer object.Body.Close()
	if captured == nil || aws.ToString(captured.Range) != "bytes=2-8" {
		t.Fatalf("GetObject Range = %q", aws.ToString(captured.Range))
	}
	if object.Size != 7 || object.ContentType != "text/plain" || object.ContentRange != "bytes 2-8/10" {
		t.Fatalf("Open() object = %+v", object)
	}
	if object.ETag != "etag" || object.VersionID != "version" {
		t.Fatalf("Open() object identity = %+v", object)
	}
}

func TestS3BucketOpenRejectsInvalidRangeBeforeS3(t *testing.T) {
	t.Parallel()

	store := testBucket(&fakeS3API{}, nil, "us-east-1")
	tests := []ByteRange{
		{Offset: -1, Length: 1},
		{Offset: 0, Length: 0},
		{Offset: 0, Length: -2},
		{Offset: 1<<63 - 2, Length: 3},
	}
	for _, byteRange := range tests {
		if _, err := store.Open(context.Background(), "objects/file", &byteRange); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("Open(%+v) error = %v, want ErrInvalidRange", byteRange, err)
		}
	}
}

func TestS3BucketCopyUsesEscapedSourceAndReturnsVersion(t *testing.T) {
	t.Parallel()

	var captured *s3.CopyObjectInput
	client := &fakeS3API{copyObject: func(_ context.Context, input *s3.CopyObjectInput) (*s3.CopyObjectOutput, error) {
		captured = input
		return &s3.CopyObjectOutput{
			CopyObjectResult: &types.CopyObjectResult{ETag: aws.String("\"copy-etag\"")},
			VersionId:        aws.String("copy-version"),
		}, nil
	}}
	store := testBucket(client, nil, "us-east-1")

	result, err := store.Copy(context.Background(), "source folder/a+b", "destination/file")
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	wantSource := url.PathEscape("test-bucket/source folder/a+b")
	if captured == nil || aws.ToString(captured.CopySource) != wantSource {
		t.Fatalf("CopySource = %q, want %q", aws.ToString(captured.CopySource), wantSource)
	}
	if result.ETag != "copy-etag" || result.VersionID != "copy-version" {
		t.Fatalf("Copy() result = %+v", result)
	}
}

func TestS3BucketListsOnlyExactObjectVersions(t *testing.T) {
	t.Parallel()

	page := 0
	client := &fakeS3API{listObjectVersions: func(_ context.Context, input *s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
		if aws.ToString(input.Prefix) != "objects/file" {
			t.Fatalf("ListObjectVersions Prefix = %q", aws.ToString(input.Prefix))
		}
		page++
		if page == 1 {
			return &s3.ListObjectVersionsOutput{
				IsTruncated:         aws.Bool(true),
				NextKeyMarker:       aws.String("objects/file"),
				NextVersionIdMarker: aws.String("version-2"),
				Versions: []types.ObjectVersion{
					{Key: aws.String("objects/file"), VersionId: aws.String("version-2")},
					{Key: aws.String("objects/file-neighbor"), VersionId: aws.String("neighbor")},
				},
			}, nil
		}
		return &s3.ListObjectVersionsOutput{DeleteMarkers: []types.DeleteMarkerEntry{
			{Key: aws.String("objects/file"), VersionId: aws.String("delete-marker")},
		}}, nil
	}}
	store := testBucket(client, nil, "us-east-1")

	versions, err := store.listObjectVersions(context.Background(), "objects/file")
	if err != nil {
		t.Fatalf("ListObjectVersions() error = %v", err)
	}
	if got := strings.Join(versions, ","); got != "delete-marker,version-2" {
		t.Fatalf("ListObjectVersions() = %q", got)
	}
}

func TestS3BucketDeletePassesVersionID(t *testing.T) {
	t.Parallel()

	var captured *s3.DeleteObjectInput
	client := &fakeS3API{deleteObject: func(_ context.Context, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
		captured = input
		return &s3.DeleteObjectOutput{}, nil
	}}
	store := testBucket(client, nil, "us-east-1")

	if err := store.Delete(context.Background(), "objects/file", DeleteOptions{VersionID: "version-2"}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if captured == nil || aws.ToString(captured.VersionId) != "version-2" {
		t.Fatalf("DeleteObject VersionId = %q", aws.ToString(captured.VersionId))
	}
}

func TestS3BucketDeleteAllVersions(t *testing.T) {
	t.Parallel()

	var deletedVersions []string
	client := &fakeS3API{
		getBucketVersioning: func(context.Context, *s3.GetBucketVersioningInput) (*s3.GetBucketVersioningOutput, error) {
			return &s3.GetBucketVersioningOutput{Status: types.BucketVersioningStatusEnabled}, nil
		},
		listObjectVersions: func(context.Context, *s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			return &s3.ListObjectVersionsOutput{
				Versions: []types.ObjectVersion{
					{Key: aws.String("objects/file"), VersionId: aws.String("version-2")},
					{Key: aws.String("objects/file"), VersionId: aws.String("version-1")},
				},
				DeleteMarkers: []types.DeleteMarkerEntry{
					{Key: aws.String("objects/file"), VersionId: aws.String("delete-marker")},
				},
			}, nil
		},
		deleteObject: func(_ context.Context, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
			versionID := aws.ToString(input.VersionId)
			deletedVersions = append(deletedVersions, versionID)
			if versionID == "version-1" {
				return nil, &smithy.GenericAPIError{Code: "NoSuchVersion", Message: "already gone"}
			}
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	err := testBucket(client, nil, "us-east-1").Delete(context.Background(), "objects/file", DeleteOptions{AllVersions: true})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got, want := strings.Join(deletedVersions, ","), "delete-marker,version-1,version-2"; got != want {
		t.Fatalf("deleted versions = %q, want %q", got, want)
	}
}

func TestS3BucketDeleteAllVersionsFallsBackForUnversionedBucket(t *testing.T) {
	t.Parallel()

	listCalls := 0
	var captured *s3.DeleteObjectInput
	client := &fakeS3API{
		listObjectVersions: func(context.Context, *s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			listCalls++
			return &s3.ListObjectVersionsOutput{}, nil
		},
		deleteObject: func(_ context.Context, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
			captured = input
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	err := testBucket(client, nil, "us-east-1").Delete(context.Background(), "objects/file", DeleteOptions{AllVersions: true})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if listCalls != 0 {
		t.Fatalf("ListObjectVersions calls = %d, want 0", listCalls)
	}
	if captured == nil || captured.VersionId != nil {
		t.Fatalf("DeleteObject VersionId = %q, want empty", aws.ToString(captured.VersionId))
	}
}

func TestS3BucketRejectsConflictingDeleteOptions(t *testing.T) {
	t.Parallel()

	err := testBucket(&fakeS3API{}, nil, "us-east-1").Delete(context.Background(), "objects/file", DeleteOptions{
		VersionID:   "version-2",
		AllVersions: true,
	})
	if !errors.Is(err, ErrInvalidDeleteOptions) {
		t.Fatalf("Delete() error = %v, want ErrInvalidDeleteOptions", err)
	}
}

func TestS3BucketNormalizesProviderErrors(t *testing.T) {
	t.Parallel()

	providerErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
	uploader := &fakeUploader{upload: func(context.Context, *transfermanager.UploadObjectInput) (*transfermanager.UploadObjectOutput, error) {
		return nil, providerErr
	}}
	store := testBucket(&fakeS3API{}, uploader, "us-east-1")

	_, err := store.Upload(context.Background(), "objects/file", strings.NewReader("body"), UploadOptions{Size: -1})
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Upload() error = %v, want ErrAccessDenied", err)
	}
	var storeError *StoreError
	if !errors.As(err, &storeError) {
		t.Fatalf("Upload() error type = %T, want *StoreError", err)
	}
	if storeError.Operation != "upload" || storeError.Bucket != "test-bucket" || storeError.Key != "objects/file" {
		t.Fatalf("StoreError = %+v", storeError)
	}
}

type fakeS3API struct {
	headBucket          func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error)
	createBucket        func(context.Context, *s3.CreateBucketInput) (*s3.CreateBucketOutput, error)
	getObject           func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error)
	deleteObject        func(context.Context, *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error)
	copyObject          func(context.Context, *s3.CopyObjectInput) (*s3.CopyObjectOutput, error)
	getBucketVersioning func(context.Context, *s3.GetBucketVersioningInput) (*s3.GetBucketVersioningOutput, error)
	listObjectVersions  func(context.Context, *s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error)
}

func (f *fakeS3API) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if f.headBucket == nil {
		return &s3.HeadBucketOutput{}, nil
	}
	return f.headBucket(ctx, input)
}

func (f *fakeS3API) CreateBucket(ctx context.Context, input *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	if f.createBucket == nil {
		return &s3.CreateBucketOutput{}, nil
	}
	return f.createBucket(ctx, input)
}

func (f *fakeS3API) GetObject(ctx context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.getObject == nil {
		return nil, errors.New("unexpected GetObject call")
	}
	return f.getObject(ctx, input)
}

func (f *fakeS3API) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.deleteObject == nil {
		return nil, errors.New("unexpected DeleteObject call")
	}
	return f.deleteObject(ctx, input)
}

func (f *fakeS3API) CopyObject(ctx context.Context, input *s3.CopyObjectInput, _ ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	if f.copyObject == nil {
		return nil, errors.New("unexpected CopyObject call")
	}
	return f.copyObject(ctx, input)
}

func (f *fakeS3API) GetBucketVersioning(ctx context.Context, input *s3.GetBucketVersioningInput, _ ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error) {
	if f.getBucketVersioning == nil {
		return &s3.GetBucketVersioningOutput{}, nil
	}
	return f.getBucketVersioning(ctx, input)
}

func (f *fakeS3API) ListObjectVersions(ctx context.Context, input *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	if f.listObjectVersions == nil {
		return &s3.ListObjectVersionsOutput{}, nil
	}
	return f.listObjectVersions(ctx, input)
}

type fakeUploader struct {
	upload func(context.Context, *transfermanager.UploadObjectInput) (*transfermanager.UploadObjectOutput, error)
}

func (*fakeUploader) GetObject(context.Context, *transfermanager.GetObjectInput, ...func(*transfermanager.Options)) (*transfermanager.GetObjectOutput, error) {
	return nil, errors.New("unexpected GetObject call")
}

type failingMultipartS3API struct {
	uploadErr  error
	abortCalls int
}

type recordingMultipartS3API struct {
	mu             sync.Mutex
	createCalls    int
	uploadCalls    int
	completeCalls  int
	abortCalls     int
	uploadedBytes  int64
	completedParts int
}

func (*failingMultipartS3API) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return nil, errors.New("unexpected PutObject call")
}

func (f *failingMultipartS3API) UploadPart(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	return nil, f.uploadErr
}

func (*failingMultipartS3API) CreateMultipartUpload(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String("upload-id")}, nil
}

func (*failingMultipartS3API) CompleteMultipartUpload(context.Context, *s3.CompleteMultipartUploadInput, ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	return nil, errors.New("unexpected CompleteMultipartUpload call")
}

func (f *failingMultipartS3API) AbortMultipartUpload(_ context.Context, input *s3.AbortMultipartUploadInput, _ ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	if aws.ToString(input.UploadId) == "upload-id" {
		f.abortCalls++
	}
	return &s3.AbortMultipartUploadOutput{}, nil
}

func (*failingMultipartS3API) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return nil, errors.New("unexpected GetObject call")
}

func (*failingMultipartS3API) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return nil, errors.New("unexpected HeadObject call")
}

func (*failingMultipartS3API) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return nil, errors.New("unexpected ListObjectsV2 call")
}

func (*recordingMultipartS3API) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return nil, errors.New("unexpected PutObject call")
}

func (f *recordingMultipartS3API) UploadPart(_ context.Context, input *s3.UploadPartInput, _ ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.uploadCalls++
	f.uploadedBytes += int64(len(body))
	f.mu.Unlock()
	return &s3.UploadPartOutput{ETag: aws.String(fmt.Sprintf("part-%d", aws.ToInt32(input.PartNumber)))}, nil
}

func (f *recordingMultipartS3API) CreateMultipartUpload(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	f.mu.Lock()
	f.createCalls++
	f.mu.Unlock()
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String("upload-id")}, nil
}

func (f *recordingMultipartS3API) CompleteMultipartUpload(_ context.Context, input *s3.CompleteMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	f.mu.Lock()
	f.completeCalls++
	if input.MultipartUpload != nil {
		f.completedParts = len(input.MultipartUpload.Parts)
	}
	f.mu.Unlock()
	return &s3.CompleteMultipartUploadOutput{}, nil
}

func (f *recordingMultipartS3API) AbortMultipartUpload(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	f.mu.Lock()
	f.abortCalls++
	f.mu.Unlock()
	return &s3.AbortMultipartUploadOutput{}, nil
}

func (*recordingMultipartS3API) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return nil, errors.New("unexpected GetObject call")
}

func (*recordingMultipartS3API) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return nil, errors.New("unexpected HeadObject call")
}

func (*recordingMultipartS3API) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return nil, errors.New("unexpected ListObjectsV2 call")
}

func (f *recordingMultipartS3API) snapshot() (int, int, int, int, int64, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls, f.uploadCalls, f.completeCalls, f.abortCalls, f.uploadedBytes, f.completedParts
}

func (f *fakeUploader) UploadObject(ctx context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	return f.upload(ctx, input)
}

type testHTTPStatusError int

func (e testHTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP status %d", e)
}

func (e testHTTPStatusError) HTTPStatusCode() int {
	return int(e)
}

type testS3Error struct {
	status int
	code   string
}

func (e testS3Error) Error() string {
	return fmt.Sprintf("S3 error %s with HTTP status %d", e.code, e.status)
}

func (e testS3Error) HTTPStatusCode() int {
	return e.status
}

func (e testS3Error) ErrorCode() string {
	return e.code
}

func testBucket(client s3API, uploader transferManagerAPI, region string) *s3Store {
	return &s3Store{client: client, uploader: uploader, name: "test-bucket", region: region}
}

func notFoundThenCreateClient(t *testing.T, assertInput func(*s3.CreateBucketInput)) *fakeS3API {
	t.Helper()
	return &fakeS3API{
		headBucket: func(context.Context, *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
			return nil, testHTTPStatusError(http.StatusNotFound)
		},
		createBucket: func(_ context.Context, input *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
			if aws.ToString(input.Bucket) != "test-bucket" {
				t.Fatalf("CreateBucket bucket = %q", aws.ToString(input.Bucket))
			}
			assertInput(input)
			return &s3.CreateBucketOutput{}, nil
		},
	}
}

func assertUploadInput(t *testing.T, input *transfermanager.UploadObjectInput, key, contentType string) {
	t.Helper()
	if input == nil {
		t.Fatal("UploadObject input = nil")
	}
	if aws.ToString(input.Bucket) != "test-bucket" || aws.ToString(input.Key) != key {
		t.Fatalf("UploadObject input = bucket %q key %q", aws.ToString(input.Bucket), aws.ToString(input.Key))
	}
	if aws.ToString(input.ContentType) != contentType {
		t.Fatalf("ContentType = %q, want %q", aws.ToString(input.ContentType), contentType)
	}
}
