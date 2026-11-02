package brainattachments

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestImageApplicationErrors(t *testing.T) {
	for _, test := range []struct {
		err      error
		kind     apperr.Kind
		sentinel error
	}{
		{invalidImage("Invalid image"), apperr.InvalidArgument, errImageInvalid},
		{imageTooLarge(), apperr.RequestTooLarge, errImageTooLarge},
		{attachmentFailure(db.ErrNotFound), apperr.NotFound, errImageNotFound},
		{attachmentFailure(errors.New("private storage details")), apperr.Unavailable, errImageUnavailable},
	} {
		var app *apperr.Error
		if !errors.As(test.err, &app) || app.Kind != test.kind || !errors.Is(test.err, test.sentinel) {
			t.Fatalf("unexpected application error: %v", test.err)
		}
		if app.PublicMessage == "private storage details" {
			t.Fatal("private failure exposed")
		}
	}
}
