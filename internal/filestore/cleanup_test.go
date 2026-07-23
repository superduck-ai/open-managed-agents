package filestore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestRunFilestoreCleanupOnceFailsMismatchedBucketWithoutDeleting(t *testing.T) {
	t.Parallel()

	database := &fakeFilestoreCleanupDatabase{jobs: []db.FilestoreObjectCleanupJob{{
		ID:         1,
		ExternalID: "cleanup-1",
		Bucket:     "unexpected-bucket",
		Key:        "objects/a",
		VersionID:  "version-1",
	}}}
	store := &fakeCleanupBlobStore{}

	err := RunFilestoreCleanupOnce(
		context.Background(),
		database,
		store,
		filestoreTestConfig(0, 0, "configured-bucket"),
		"worker-1",
	)

	if err != nil {
		t.Fatalf("RunFilestoreCleanupOnce() error = %v", err)
	}
	if len(store.deleteCalls) != 0 {
		t.Fatalf("Delete calls = %+v", store.deleteCalls)
	}
	if len(database.failures) != 1 {
		t.Fatalf("failures = %+v", database.failures)
	}
	failure := database.failures[0]
	if failure.jobID != 1 || failure.workerID != "worker-1" || failure.delay != time.Hour || failure.maxAttempts != filestoreCleanupMaxAttempts {
		t.Fatalf("failure = %+v", failure)
	}
	if failure.reason != "cleanup bucket does not match configured Filestore bucket" {
		t.Fatalf("failure reason = %q", failure.reason)
	}
}

func TestRunFilestoreCleanupOnceSchedulesDeleteFailureRetry(t *testing.T) {
	t.Parallel()

	database := &fakeFilestoreCleanupDatabase{jobs: []db.FilestoreObjectCleanupJob{{
		ID:         2,
		ExternalID: "cleanup-2",
		Bucket:     "configured-bucket",
		Key:        "objects/b",
		VersionID:  "version-2",
		Attempts:   2,
	}}}
	deleteErr := errors.New("object store unavailable")
	store := &fakeCleanupBlobStore{deleteError: deleteErr}

	err := RunFilestoreCleanupOnce(
		context.Background(),
		database,
		store,
		filestoreTestConfig(0, 0, "configured-bucket"),
		"worker-2",
	)

	if err != nil {
		t.Fatalf("RunFilestoreCleanupOnce() error = %v", err)
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0].key != "objects/b" || store.deleteCalls[0].versionID != "version-2" {
		t.Fatalf("Delete calls = %+v", store.deleteCalls)
	}
	if len(database.failures) != 1 {
		t.Fatalf("failures = %+v", database.failures)
	}
	failure := database.failures[0]
	if failure.jobID != 2 || failure.workerID != "worker-2" || failure.reason != deleteErr.Error() {
		t.Fatalf("failure = %+v", failure)
	}
	if failure.delay != 9*time.Minute || failure.maxAttempts != filestoreCleanupMaxAttempts {
		t.Fatalf("failure retry settings = %+v", failure)
	}
	if len(database.completed) != 0 {
		t.Fatalf("completed jobs = %v", database.completed)
	}
}

func TestRunFilestoreCleanupOnceCompletesMissingObject(t *testing.T) {
	t.Parallel()

	database := &fakeFilestoreCleanupDatabase{jobs: []db.FilestoreObjectCleanupJob{{
		ID:         3,
		ExternalID: "cleanup-3",
		Bucket:     "configured-bucket",
		Key:        "objects/missing",
		VersionID:  "version-3",
	}}}
	store := &fakeCleanupBlobStore{deleteError: errors.Join(errors.New("delete failed"), storage.ErrNotFound)}

	err := RunFilestoreCleanupOnce(
		context.Background(),
		database,
		store,
		filestoreTestConfig(0, 0, "configured-bucket"),
		"worker-3",
	)

	if err != nil {
		t.Fatalf("RunFilestoreCleanupOnce() error = %v", err)
	}
	if len(database.completed) != 1 || database.completed[0] != 3 {
		t.Fatalf("completed jobs = %v", database.completed)
	}
	if len(database.completedWorkerIDs) != 1 || database.completedWorkerIDs[0] != "worker-3" {
		t.Fatalf("completed worker IDs = %v", database.completedWorkerIDs)
	}
	if len(database.failures) != 0 {
		t.Fatalf("failures = %+v", database.failures)
	}
	if database.leasedWorkerID != "worker-3" || database.leasedLimit != filestoreCleanupBatchSize {
		t.Fatalf("lease worker = %q, limit = %d", database.leasedWorkerID, database.leasedLimit)
	}
}

func TestRunFilestoreCleanupOnceReturnsStateTransitionErrors(t *testing.T) {
	t.Parallel()

	completeErr := errors.New("complete failed")
	failErr := errors.New("fail transition failed")
	database := &fakeFilestoreCleanupDatabase{
		jobs: []db.FilestoreObjectCleanupJob{
			{ID: 4, ExternalID: "cleanup-4", Bucket: "configured-bucket", Key: "objects/complete"},
			{ID: 5, ExternalID: "cleanup-5", Bucket: "wrong-bucket", Key: "objects/fail"},
		},
		completeError: completeErr,
		failError:     failErr,
	}
	store := &fakeCleanupBlobStore{}

	err := RunFilestoreCleanupOnce(
		context.Background(),
		database,
		store,
		filestoreTestConfig(0, 0, "configured-bucket"),
		"worker-4",
	)

	if !errors.Is(err, completeErr) || !errors.Is(err, failErr) {
		t.Fatalf("RunFilestoreCleanupOnce() error = %v", err)
	}
	if !strings.Contains(err.Error(), "cleanup-4") || !strings.Contains(err.Error(), "cleanup-5") {
		t.Fatalf("error lacks job context: %v", err)
	}
	if len(store.deleteCalls) != 1 || !store.deleteCalls[0].allVersions {
		t.Fatalf("Delete calls = %+v, want all-version cleanup for a job without version ID", store.deleteCalls)
	}
}

func TestRunFilestoreCleanupOnceReturnsLeaseError(t *testing.T) {
	t.Parallel()

	leaseErr := errors.New("lease failed")
	database := &fakeFilestoreCleanupDatabase{leaseError: leaseErr}

	err := RunFilestoreCleanupOnce(
		context.Background(),
		database,
		&fakeCleanupBlobStore{},
		filestoreTestConfig(0, 0, "configured-bucket"),
		"worker-5",
	)

	if !errors.Is(err, leaseErr) {
		t.Fatalf("RunFilestoreCleanupOnce() error = %v", err)
	}
}

func TestRunFilestoreCleanupOnceStopsOnCanceledDelete(t *testing.T) {
	t.Parallel()

	database := &fakeFilestoreCleanupDatabase{jobs: []db.FilestoreObjectCleanupJob{
		{ID: 6, ExternalID: "cleanup-6", Bucket: "configured-bucket", Key: "objects/canceled"},
		{ID: 7, ExternalID: "cleanup-7", Bucket: "configured-bucket", Key: "objects/not-reached"},
	}}
	store := &fakeCleanupBlobStore{deleteError: context.Canceled}

	err := RunFilestoreCleanupOnce(
		context.Background(),
		database,
		store,
		filestoreTestConfig(0, 0, "configured-bucket"),
		"worker-6",
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunFilestoreCleanupOnce() error = %v", err)
	}
	if len(store.deleteCalls) != 1 {
		t.Fatalf("Delete calls = %+v", store.deleteCalls)
	}
	if !store.deleteCalls[0].allVersions {
		t.Fatalf("Delete call = %+v, want all-version cleanup for a job without version ID", store.deleteCalls[0])
	}
	if len(database.completed) != 0 || len(database.failures) != 0 {
		t.Fatalf("completed = %v, failures = %+v", database.completed, database.failures)
	}
}

func TestRunFilestoreTTLSweepOnceUsesBoundedBatch(t *testing.T) {
	t.Parallel()

	database := &fakeFilestoreCleanupDatabase{}

	if err := RunFilestoreTTLSweepOnce(context.Background(), database); err != nil {
		t.Fatalf("RunFilestoreTTLSweepOnce() error = %v", err)
	}
	if database.expireCalls != 1 || database.expireLimit != filestoreTTLSweepBatchSize {
		t.Fatalf("ExpireFilestoreEntries calls = %d, limit = %d", database.expireCalls, database.expireLimit)
	}
}

func TestRunFilestoreTTLSweepOnceReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	expireErr := errors.New("expiry failed")
	database := &fakeFilestoreCleanupDatabase{expireError: expireErr}

	err := RunFilestoreTTLSweepOnce(context.Background(), database)

	if !errors.Is(err, expireErr) {
		t.Fatalf("RunFilestoreTTLSweepOnce() error = %v", err)
	}
}

func TestRunFilestoreFilesystemCleanupOnceSchedulesProcessFailureRetry(t *testing.T) {
	t.Parallel()

	processErr := errors.New("database unavailable")
	database := &fakeFilestoreCleanupDatabase{
		filesystemJobs: []db.FilestoreFilesystemCleanupJob{{
			ID:         8,
			ExternalID: "filesystem-cleanup-8",
			Attempts:   1,
		}},
		filesystemProcessError: processErr,
	}

	err := RunFilestoreFilesystemCleanupOnce(context.Background(), database, "worker-8")
	if err != nil {
		t.Fatalf("RunFilestoreFilesystemCleanupOnce() error = %v", err)
	}
	if len(database.filesystemFailures) != 1 {
		t.Fatalf("filesystem failures = %+v", database.filesystemFailures)
	}
	failure := database.filesystemFailures[0]
	if failure.jobID != 8 || failure.workerID != "worker-8" || failure.reason != processErr.Error() {
		t.Fatalf("filesystem failure = %+v", failure)
	}
	if failure.delay != 4*time.Minute || failure.maxAttempts != filestoreCleanupMaxAttempts {
		t.Fatalf("filesystem retry settings = %+v", failure)
	}
}

func TestRunFilestoreFilesystemCleanupOnceUsesBoundedBatch(t *testing.T) {
	t.Parallel()

	database := &fakeFilestoreCleanupDatabase{filesystemJobs: []db.FilestoreFilesystemCleanupJob{{ID: 9}}}

	if err := RunFilestoreFilesystemCleanupOnce(context.Background(), database, "worker-9"); err != nil {
		t.Fatalf("RunFilestoreFilesystemCleanupOnce() error = %v", err)
	}
	if database.filesystemLeasedWorkerID != "worker-9" || database.filesystemLeasedLimit != filestoreCleanupBatchSize {
		t.Fatalf("filesystem lease worker = %q, limit = %d", database.filesystemLeasedWorkerID, database.filesystemLeasedLimit)
	}
	if len(database.filesystemProcessed) != 1 || database.filesystemProcessed[0] != 9 {
		t.Fatalf("processed filesystem jobs = %v", database.filesystemProcessed)
	}
	if database.filesystemProcessLimit != filestoreFilesystemCleanupBatchSize {
		t.Fatalf("filesystem process limit = %d", database.filesystemProcessLimit)
	}
}

type cleanupDeleteCall struct {
	key         string
	versionID   string
	allVersions bool
}

type fakeCleanupBlobStore struct {
	storage.ObjectStore
	deleteCalls []cleanupDeleteCall
	deleteError error
}

func (*fakeCleanupBlobStore) Name() string { return "configured-bucket" }

func (s *fakeCleanupBlobStore) Delete(_ context.Context, key string, options storage.DeleteOptions) error {
	s.deleteCalls = append(s.deleteCalls, cleanupDeleteCall{
		key:         key,
		versionID:   options.VersionID,
		allVersions: options.AllVersions,
	})
	return s.deleteError
}

type cleanupFailure struct {
	jobID       int64
	workerID    string
	reason      string
	delay       time.Duration
	maxAttempts int
}

type fakeFilestoreCleanupDatabase struct {
	jobs                     []db.FilestoreObjectCleanupJob
	leaseError               error
	leasedWorkerID           string
	leasedLimit              int
	completed                []int64
	completedWorkerIDs       []string
	completeError            error
	failures                 []cleanupFailure
	failError                error
	expireCalls              int
	expireLimit              int
	expireError              error
	filesystemJobs           []db.FilestoreFilesystemCleanupJob
	filesystemLeaseError     error
	filesystemLeasedWorkerID string
	filesystemLeasedLimit    int
	filesystemProcessed      []int64
	filesystemProcessLimit   int
	filesystemProcessError   error
	filesystemFailures       []cleanupFailure
}

func (d *fakeFilestoreCleanupDatabase) LeaseFilestoreFilesystemCleanupJobs(
	_ context.Context,
	workerID string,
	limit int,
) ([]db.FilestoreFilesystemCleanupJob, error) {
	d.filesystemLeasedWorkerID = workerID
	d.filesystemLeasedLimit = limit
	return d.filesystemJobs, d.filesystemLeaseError
}

func (d *fakeFilestoreCleanupDatabase) ProcessLeasedFilestoreFilesystemCleanupJob(
	_ context.Context,
	jobID int64,
	_ string,
	limit int,
) (bool, error) {
	d.filesystemProcessed = append(d.filesystemProcessed, jobID)
	d.filesystemProcessLimit = limit
	return d.filesystemProcessError == nil, d.filesystemProcessError
}

func (d *fakeFilestoreCleanupDatabase) FailLeasedFilestoreFilesystemCleanupJob(
	_ context.Context,
	jobID int64,
	workerID string,
	reason string,
	retryDelay time.Duration,
	maxAttempts int,
) error {
	d.filesystemFailures = append(d.filesystemFailures, cleanupFailure{
		jobID:       jobID,
		workerID:    workerID,
		reason:      reason,
		delay:       retryDelay,
		maxAttempts: maxAttempts,
	})
	return d.failError
}

func (d *fakeFilestoreCleanupDatabase) LeaseFilestoreObjectCleanupJobs(
	_ context.Context,
	workerID string,
	limit int,
) ([]db.FilestoreObjectCleanupJob, error) {
	d.leasedWorkerID = workerID
	d.leasedLimit = limit
	return d.jobs, d.leaseError
}

func (d *fakeFilestoreCleanupDatabase) CompleteLeasedFilestoreObjectCleanupJob(_ context.Context, jobID int64, workerID string) error {
	d.completed = append(d.completed, jobID)
	d.completedWorkerIDs = append(d.completedWorkerIDs, workerID)
	return d.completeError
}

func (d *fakeFilestoreCleanupDatabase) FailLeasedFilestoreObjectCleanupJob(
	_ context.Context,
	jobID int64,
	workerID string,
	reason string,
	retryDelay time.Duration,
	maxAttempts int,
) error {
	d.failures = append(d.failures, cleanupFailure{
		jobID:       jobID,
		workerID:    workerID,
		reason:      reason,
		delay:       retryDelay,
		maxAttempts: maxAttempts,
	})
	return d.failError
}

func (d *fakeFilestoreCleanupDatabase) ExpireFilestoreEntries(
	_ context.Context,
	limit int,
) ([]db.FilestoreObjectCleanupJob, error) {
	d.expireCalls++
	d.expireLimit = limit
	return nil, d.expireError
}
