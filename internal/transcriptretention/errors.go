package transcriptretention

import (
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptarchive"
)

var (
	errStorage       = errors.New("transcript archive storage unavailable")
	errIntegrity     = errors.New("transcript archive does not match persisted events")
	errArchiveMinAge = errors.New("transcript archive_min_age must be at least 7 days")
)

func deletionNeedsRepair(err error) bool {
	return errors.Is(err, db.ErrInvalidState) || errors.Is(err, errIntegrity) || errors.Is(err, errStorage) ||
		errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrAccessDenied) ||
		errors.Is(err, transcriptarchive.ErrIntegrity) || errors.Is(err, transcriptarchive.ErrInvalidSegment)
}
