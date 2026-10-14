package tests

import (
	"context"
	"testing"
)

func TestVerifyTranscriptConcurrency(t *testing.T) {
	f := newTranscriptVerification(t, 8, true)
	uploaded, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	defer func() {
		close(release)
		cancel()
		<-finished
	}()
	f.objects.afterUpload = func() error {
		close(uploaded)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() {
		defer close(finished)
		done <- f.service.Archive(ctx, f.scope, true)
	}()
	select {
	case <-uploaded:
	case <-t.Context().Done():
		t.Fatal("BE_TIMEOUT waiting for concurrent archive upload")
	}
	pending := f.segment(t, "pending")
	f.rows(t, f.count, f.count)
	f.archive(t)
	f.rows(t, f.count, 0)
	transcriptVerifyProof(t, f.started, "concurrent_archive_coordinated")
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.segment(t, "attached").UUID != pending.UUID || f.objects.uploads.Load() != 1 {
		t.Fatal("concurrent archive created duplicate manifests or uploads")
	}
	f.assertExport(t)
	transcriptVerifyProof(t, f.started, "single_manifest_preserved")
	f.archive(t)
	f.ageDeleted(t)
	f.hardDelete(t)
	f.rows(t, 0, 0)
	if err := f.service.Restore(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}
	f.rows(t, f.count, f.count)
	f.assertExport(t)
	transcriptVerifyProof(t, f.started, "concurrent_retry_matches")
}
