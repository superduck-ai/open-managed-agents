package channels

import (
	"bytes"
	"context"
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
	if settings.EnvironmentID == nil {
		return environmentRequired()
	}
	id := strings.TrimSpace(*settings.EnvironmentID)
	if err := h.checkEnvironment(r.Context(), workspaceUUID, id); err != nil {
		return err
	}
	settings.EnvironmentID = &id

	body, err := json.Marshal(settings)
	if err != nil {
		return invalidEnvironment(err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}

func (h *handler) checkEnvironment(ctx context.Context, workspaceUUID, id string) error {
	if id == "" {
		return environmentRequired()
	}
	env, err := h.store.GetEnvironment(ctx, workspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) || (err == nil && env.ArchivedAt != nil) {
		return invalidEnvironment(err)
	}
	if err != nil {
		return unavailable(err)
	}
	return nil
}

func (h *handler) requireEnvironment(upstream *http.Request, workspaceUUID string) error {
	endpoint, err := h.endpoint(upstream, true)
	if err != nil {
		return err
	}
	request := upstream.Clone(upstream.Context())
	request.Method, request.Body, request.GetBody, request.ContentLength = http.MethodGet, nil, nil, 0
	request.URL = endpoint
	response, err := h.client.Do(request)
	if err != nil {
		return unavailable(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return unavailable(nil)
	}
	var settings environmentSettings
	if err := json.UnmarshalRead(io.LimitReader(response.Body, 1<<20), &settings); err != nil {
		return unavailable(err)
	}
	if settings.EnvironmentID == nil {
		return environmentRequired()
	}
	return h.checkEnvironment(upstream.Context(), workspaceUUID, *settings.EnvironmentID)
}
