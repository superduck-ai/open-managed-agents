package transcriptretention

import (
	"testing"
	"time"

	"github.com/riverqueue/river"
)

func TestStorageWorkersOverrideClientTimeout(t *testing.T) {
	var archive river.Worker[archiveArgs] = &archiveWorker{}
	var deletion river.Worker[deleteArgs] = &deleteWorker{}
	for name, timeout := range map[string]time.Duration{
		"archive": archive.Timeout(&river.Job[archiveArgs]{}),
		"delete":  deletion.Timeout(&river.Job[deleteArgs]{}),
	} {
		if timeout <= 2*time.Minute || timeout > 10*time.Minute {
			t.Fatalf("%s timeout %s must allow multiple storage operations and stay bounded", name, timeout)
		}
	}
	var sweep river.Worker[sweepArgs] = &sweepWorker{}
	if sweep.Timeout(&river.Job[sweepArgs]{}) != 0 {
		t.Fatal("sweep must retain the shared client timeout")
	}
}
