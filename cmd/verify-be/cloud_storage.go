package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func cloudStore(cfg config.S3Config) (storage.ObjectStore, error) {
	client, err := storage.New(config.StorageConfig{Type: "s3", S3: cfg})
	if err != nil {
		return nil, errors.New("cannot configure cloud storage adapter")
	}
	store, err := client.ForBucket(cfg.Bucket)
	if err != nil {
		return nil, errors.New("cannot select cloud storage bucket")
	}
	return store, nil
}

func verifyCloudStorage(ctx context.Context, cfg cloudConfig, directory string, r *report, stage func(string)) (result error) {
	owner, err := cloudStore(cfg.Storage)
	if err != nil {
		return err
	}
	readerCfg := cfg.Storage
	readerCfg.AccessKeyID, readerCfg.SecretAccessKey = cfg.ReadOnly.AccessKeyID, cfg.ReadOnly.SecretAccessKey
	reader, err := cloudStore(readerCfg)
	if err != nil {
		return err
	}
	key := "verify-be/" + r.RunID + "/content.bin"
	if err := saveCloudResources(directory, cloudResources{ObjectKey: key}); err != nil {
		return err
	}
	r.CleanupComplete = false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cleanupErr := owner.Delete(cleanupCtx, key, storage.DeleteOptions{AllVersions: true})
		if cleanupErr == nil {
			object, openErr := owner.Open(cleanupCtx, key, nil)
			if openErr == nil {
				_ = object.Body.Close()
				cleanupErr = errors.New("object remains")
			} else if !errors.Is(openErr, storage.ErrNotFound) {
				cleanupErr = errors.New("cannot confirm object absence")
			}
		}
		cloudCleanup(r, cleanupErr)
		if cleanupErr != nil {
			result = errors.Join(result, errors.New("cloud storage cleanup failed"))
			return
		}
		r.CleanupComplete = true
		if result == nil {
			stage("cloud_storage_cleaned")
		}
	}()
	content := bytes.Repeat([]byte("verify-be-cloud"), 128)
	if _, err := owner.Upload(ctx, key, bytes.NewReader(content), storage.UploadOptions{Size: int64(len(content)), ContentType: "application/octet-stream"}); err != nil {
		return errors.New("cloud owner upload failed")
	}
	if err := verifyCloudBytes(ctx, owner, key, content); err != nil {
		return err
	}
	if err := verifyCloudBytes(ctx, reader, key, content); err != nil {
		return err
	}
	stage("cloud_storage_roundtrip")
	if _, err := reader.Upload(ctx, key, bytes.NewReader([]byte("must be denied")), storage.UploadOptions{Size: 14}); !errors.Is(err, storage.ErrAccessDenied) {
		return errors.New("read-only principal did not reject PUT with access denied")
	}
	if err := reader.Delete(ctx, key, storage.DeleteOptions{}); !errors.Is(err, storage.ErrAccessDenied) {
		return errors.New("read-only principal did not reject DELETE with access denied")
	}
	if err := verifyCloudBytes(ctx, owner, key, content); err != nil {
		return err
	}
	stage("cloud_readonly_enforced")
	return nil
}

func verifyCloudBytes(ctx context.Context, store storage.ObjectStore, key string, content []byte) error {
	object, err := store.Open(ctx, key, nil)
	if err != nil {
		return errors.New("cloud object read failed")
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, int64(len(content)+1)))
	if err != nil || !bytes.Equal(data, content) {
		return errors.New("cloud object content differs")
	}
	return nil
}
