package livefiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type interruptedUpload struct{}

func (interruptedUpload) Read([]byte) (int, error) {
	return 0, errors.New("verification stream interrupted")
}

func (e *filesEnv) verifyStorageBoundaries(t *testing.T) {
	t.Helper()
	prefix := "workspaces/" + e.key.WorkspaceUUID.String() + "/files/"
	e.deny(t, "PUT", prefix, true)
	e.upload(t, "denied.bin", []byte("denied"), 500)
	if len(e.objectKeys(t)) != 0 || len(e.page(t, e.token, "").Data) != 0 {
		t.Fatal("storage access denial leaked metadata/object")
	}
	e.deny(t, "PUT", prefix, false)
	invalidCfg := e.cfg.Storage
	invalidCfg.S3.AccessKeyID = "verification-invalid-credential"
	client, err := storage.New(invalidCfg)
	requireOK(t, err)
	denied, err := client.ForBucket(e.objects.Name())
	requireOK(t, err)
	_, err = denied.Upload(e.ctx, "invalid-credential", bytes.NewReader([]byte("x")), storage.UploadOptions{Size: 1})
	if !errors.Is(err, storage.ErrAccessDenied) {
		t.Fatalf("invalid credential error=%v", err)
	}
	e.proof(t, "storage_permissions_rejected")
	download := e.downloadable(t, []byte("connection recovery"))
	rule, err := json.Marshal(struct {
		Method     string `json:"method"`
		Prefix     string `json:"prefix"`
		Disconnect bool   `json:"disconnect"`
	}{"GET", "/" + e.objects.Name() + "/" + download.S3Key, true})
	requireOK(t, err)
	e.faultRequest(t, "PUT", rule)
	e.request(t, "GET", "/v1/files/"+download.ExternalID+"/content", e.token, "", nil, true, 500)
	e.deny(t, "GET", download.S3Key, false)
	data, _ := e.request(t, "GET", "/v1/files/"+download.ExternalID+"/content", e.token, "", nil, true, 200)
	if string(data) != "connection recovery" {
		t.Fatal("download did not recover after network interruption")
	}
	e.delete(t, download)
	e.proof(t, "storage_disconnect_recovered")
	key := "multipart/interrupted"
	_, err = e.objects.Upload(e.ctx, key, io.MultiReader(bytes.NewReader(make([]byte, 32<<20)), interruptedUpload{}), storage.UploadOptions{Size: -1})
	if err == nil {
		t.Fatal("interrupted multipart upload succeeded")
	}
	unfinished, err := e.s3.ListMultipartUploads(e.ctx, &s3.ListMultipartUploadsInput{Bucket: aws.String(e.objects.Name()), Prefix: aws.String(key)})
	requireOK(t, err)
	if len(e.deleteAttempts(t, key)) != 1 {
		t.Fatal("multipart abort request was not observed")
	}
	if len(unfinished.Uploads) != 0 || !e.objectAbsent(t, key) {
		t.Fatal("interrupted multipart upload leaked data")
	}
	e.proof(t, "multipart_abort_cleaned")
	_, err = e.s3.PutBucketVersioning(e.ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(e.objects.Name()), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}})
	requireOK(t, err)
	key = "versioned/file"
	first, err := e.objects.Upload(e.ctx, key, bytes.NewBufferString("first"), storage.UploadOptions{Size: 5})
	requireOK(t, err)
	second, err := e.objects.Upload(e.ctx, key, bytes.NewBufferString("second"), storage.UploadOptions{Size: 6})
	requireOK(t, err)
	if first.VersionID == "" || second.VersionID == "" || first.VersionID == second.VersionID {
		t.Fatal("version IDs missing or unchanged")
	}
	requireOK(t, e.objects.Delete(e.ctx, key, storage.DeleteOptions{VersionID: second.VersionID}))
	e.assertObject(t, key, []byte("first"))
	requireOK(t, e.objects.Delete(e.ctx, key, storage.DeleteOptions{}))
	if !e.objectAbsent(t, key) {
		t.Fatal("delete marker did not hide object")
	}
	versions, err := e.s3.ListObjectVersions(e.ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(e.objects.Name()), Prefix: aws.String(key)})
	requireOK(t, err)
	if len(versions.Versions) != 1 || len(versions.DeleteMarkers) != 1 {
		t.Fatal("version deletion contract differs")
	}
	requireOK(t, e.objects.Delete(e.ctx, key, storage.DeleteOptions{AllVersions: true}))
	versions, err = e.s3.ListObjectVersions(e.ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(e.objects.Name()), Prefix: aws.String(key)})
	requireOK(t, err)
	if len(versions.Versions) != 0 || len(versions.DeleteMarkers) != 0 {
		t.Fatal("all-version cleanup left versions or markers")
	}
	e.proof(t, "versioned_objects_cleaned")
}
