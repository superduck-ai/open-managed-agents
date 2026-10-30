package channels

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type environmentSettings struct {
	EnvironmentID *string `json:"environment_id"`
}

func (h *handler) validateEnvironment(w http.ResponseWriter, r *http.Request, workspaceUUID string) error {
	var settings *environmentSettings
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 32<<10), &settings, json.RejectUnknownMembers(true)); err != nil {
		return invalidEnvironment(err)
	}
	if settings == nil {
		return invalidEnvironment(nil)
	}
	if settings.EnvironmentID != nil {
		id := strings.TrimSpace(*settings.EnvironmentID)
		env, err := h.store.GetEnvironment(r.Context(), workspaceUUID, id)
		if errors.Is(err, db.ErrNotFound) || (err == nil && (env.ArchivedAt != nil || env.DeletedAt != nil)) {
			return invalidEnvironment(err)
		}
		if err != nil {
			return unavailable(err)
		}
		settings.EnvironmentID = &id
	}
	body, err := json.Marshal(settings)
	if err != nil {
		return invalidEnvironment(err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}
