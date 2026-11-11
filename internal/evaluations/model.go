package evaluations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const rubricSystemPrompt = "Reply with Markdown scoring rules and no other text. Do not wrap the rubric in a JSON object."

var (
	errModelMissing = errors.New("model missing")
	errModelFailed  = errors.New("model failed")
)

type CompletionRequest struct {
	OrganizationUUID string
	WorkspaceUUID    string
	Model            string
	System           string
	User             string
}

type RubricModel interface {
	Complete(context.Context, CompletionRequest) (string, error)
}

type LLMRubricModel struct {
	database *db.DB
	secrets  *secrets.Service
	client   *http.Client
}

func NewRubricModel(database *db.DB, secretService *secrets.Service) *LLMRubricModel {
	return &LLMRubricModel{database: database, secrets: secretService, client: llmproviders.NewHTTPClient(60 * time.Second)}
}

func (m *LLMRubricModel) Complete(ctx context.Context, request CompletionRequest) (string, error) {
	modelID := strings.TrimSpace(request.Model)
	if modelID == "" {
		resolved, ok, err := firstWorkspaceModel(ctx, m.database, request.OrganizationUUID, request.WorkspaceUUID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errModelMissing
		}
		modelID = resolved
	}
	upstream, err := llmproviders.Resolve(ctx, m.database, m.secrets, request.OrganizationUUID, request.WorkspaceUUID, modelID)
	if err != nil {
		if errors.Is(err, llmproviders.ErrNotConfigured) || errors.Is(err, llmproviders.ErrModelNotConfigured) {
			return "", errModelMissing
		}
		return "", err
	}
	return m.complete(ctx, upstream, request)
}

func firstWorkspaceModel(ctx context.Context, database *db.DB, organizationUUID, workspaceUUID string) (string, bool, error) {
	catalog, err := llmproviders.LoadCatalog(ctx, database, organizationUUID, workspaceUUID)
	if err != nil {
		if errors.Is(err, llmproviders.ErrNotConfigured) {
			return "", false, nil
		}
		return "", false, err
	}
	models := catalog.ModelIDs()
	if len(models) == 0 {
		return "", false, nil
	}
	return models[0], true, nil
}

func (m *LLMRubricModel) complete(ctx context.Context, upstream llmproviders.Upstream, request CompletionRequest) (string, error) {
	backend := upstream.APIBackend
	if backend == "" {
		backend = llmproviders.Messages
	}
	path := "/v1/messages"
	if backend == llmproviders.ChatCompletions {
		path = "/v1/chat/completions"
	}
	body, err := completionBody(backend, upstream.ModelID, request.System, request.User)
	if err != nil {
		return "", err
	}
	target, err := llmproviders.Endpoint(upstream.BaseURL, path, "")
	if err != nil {
		return "", err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	upstream.ApplyHeaders(httpRequest.Header)
	response, err := m.client.Do(httpRequest)
	if err != nil {
		return "", errModelFailed
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errModelFailed
	}
	return completionText(backend, payload)
}

func completionBody(backend, model, system, user string) ([]byte, error) {
	if backend == llmproviders.ChatCompletions {
		return json.Marshal(map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
		})
	}
	return json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 2048,
		"system":     system,
		"messages": []map[string]any{{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": user}},
		}},
	})
}

func completionText(backend string, payload []byte) (string, error) {
	if backend == llmproviders.ChatCompletions {
		var decoded struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(payload, &decoded); err != nil || len(decoded.Choices) == 0 {
			return "", errModelFailed
		}
		return decoded.Choices[0].Message.Content, nil
	}
	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", errModelFailed
	}
	var builder strings.Builder
	for _, block := range decoded.Content {
		if block.Type == "" || block.Type == "text" {
			builder.WriteString(block.Text)
		}
	}
	if builder.Len() == 0 {
		return "", errModelFailed
	}
	return builder.String(), nil
}
