package tests

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptarchive"
)

func TestVerifyTranscriptIntegrity(t *testing.T) {
	requireTranscriptVerification(t)
	started := time.Now()
	for _, corrupt := range []bool{false, true} {
		name, stage := "missing", "missing_object_blocks_operations"
		if corrupt {
			name, stage = "corrupt", "corrupt_object_blocks_operations"
		}
		if !t.Run(name, func(t *testing.T) {
			f := newTranscriptVerification(t, 4, true)
			f.archive(t)
			segment := f.segment(t, "attached")
			body := transcriptObjectBytes(t, f.objects, segment.Key)
			f.ageDeleted(t)
			if err := f.objects.Delete(t.Context(), segment.Key, storage.DeleteOptions{AllVersions: true}); err != nil {
				t.Fatal(err)
			}
			expected := storage.ErrNotFound
			if corrupt {
				damaged := bytes.Clone(body)
				damaged[len(damaged)-1] ^= 1
				if _, err := f.objects.Upload(t.Context(), segment.Key, bytes.NewReader(damaged), storage.UploadOptions{Size: int64(len(damaged))}); err != nil {
					t.Fatal(err)
				}
				expected = transcriptarchive.ErrIntegrity
			}
			for _, operation := range []func() error{
				func() error { return f.service.HardDelete(t.Context(), f.scope) },
				func() error { return f.service.Restore(t.Context(), f.scope) },
				func() error { return f.service.Export(t.Context(), f.scope, &bytes.Buffer{}) },
			} {
				if err := operation(); !errors.Is(err, expected) {
					t.Fatalf("unsafe history operation: %v, want %v", err, expected)
				}
				f.rows(t, f.count, 0)
				f.segment(t, "attached")
			}
			if _, err := f.objects.Upload(t.Context(), segment.Key, bytes.NewReader(body), storage.UploadOptions{Size: int64(len(body))}); err != nil {
				t.Fatal(err)
			}
			f.assertExport(t)
			f.hardDelete(t)
			f.rows(t, 0, 0)
			if err := f.service.Restore(t.Context(), f.scope); err != nil {
				t.Fatal(err)
			}
			f.rows(t, f.count, f.count)
			f.assertExport(t)
		}) {
			t.Fatal("integrity assertion failed")
		}
		transcriptVerifyProof(t, started, stage)
	}
	transcriptVerifyProof(t, started, "repaired_object_recovers")
}
