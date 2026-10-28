package brainattachments

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

var errImageInvalid = errors.New("invalid image attachment")
var errImageCorrupt = errors.New("stored image attachment is corrupt")
var errImageTooLarge = errors.New("image attachment exceeds size limit")
var errImageNotFound = errors.New("image attachment not found")
var errImageUnavailable = errors.New("image attachment storage unavailable")

func invalidImage(message string) error {
	return apperr.New(apperr.InvalidArgument, message, errImageInvalid)
}

func imageTooLarge() error {
	return apperr.New(apperr.RequestTooLarge, "Image attachment exceeds the size limit", errImageTooLarge)
}

func attachmentFailure(err error) error {
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		return apperr.New(apperr.NotFound, "Image attachment not found", errors.Join(errImageNotFound, err))
	}
	return apperr.New(apperr.Unavailable, "Image attachment storage is unavailable", errors.Join(errImageUnavailable, err))
}

func corruptImage() error {
	return apperr.New(apperr.InvalidArgument, "Stored image attachment is corrupt", errImageCorrupt)
}
