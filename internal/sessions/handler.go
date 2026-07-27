package sessions

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
)

const maxSessionBodySize = 4 << 20

type Handler struct {
	cfg          config.Config
	db           *db.DB
	codeSessions *codesessions.Service
	webhooks     webhookEnqueuer
	logger       *slog.Logger
	router       chi.Router
	streams      *streamHub
}

type webhookEnqueuer interface {
	Enqueue(context.Context, webhooks.EnqueueInput)
}

type pageResponse[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

type sessionResponse struct {
	ID                 string            `json:"id"`
	Agent              json.RawMessage   `json:"agent"`
	ArchivedAt         *string           `json:"archived_at"`
	CreatedAt          string            `json:"created_at"`
	DeploymentID       *string           `json:"deployment_id,omitempty"`
	EnvironmentID      string            `json:"environment_id"`
	Metadata           json.RawMessage   `json:"metadata"`
	OutcomeEvaluations json.RawMessage   `json:"outcome_evaluations"`
	Resources          []json.RawMessage `json:"resources"`
	Stats              json.RawMessage   `json:"stats"`
	Status             string            `json:"status"`
	Title              *string           `json:"title"`
	Type               string            `json:"type"`
	UpdatedAt          string            `json:"updated_at"`
	Usage              json.RawMessage   `json:"usage"`
	VaultIDs           json.RawMessage   `json:"vault_ids"`
}

type threadResponse struct {
	ID             string          `json:"id"`
	Agent          json.RawMessage `json:"agent"`
	ArchivedAt     *string         `json:"archived_at"`
	CreatedAt      string          `json:"created_at"`
	ParentThreadID *string         `json:"parent_thread_id"`
	SessionID      string          `json:"session_id"`
	Stats          json.RawMessage `json:"stats"`
	Status         string          `json:"status"`
	Type           string          `json:"type"`
	UpdatedAt      string          `json:"updated_at"`
	Usage          json.RawMessage `json:"usage"`
}

type deleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type sendEventsResponse struct {
	Data []json.RawMessage `json:"data,omitempty"`
}

type resourceReferenceError struct {
	ResourceType string
	ResourceID   string
	Err          error
}

func (e resourceReferenceError) Error() string {
	return e.ResourceType + " reference failed: " + e.ResourceID
}

func (e resourceReferenceError) Unwrap() error {
	return e.Err
}
