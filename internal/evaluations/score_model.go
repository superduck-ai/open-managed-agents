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

const (
	evaluatorScoreSystemPrompt = "Reply with one JSON object and no other text. Fields are score (number), explanation (string), and details (object)."
	evaluatorScoreMaxTokens    = 1024
	evaluatorScoreBodyLimit    = 1 << 20
)

var errEvaluatorScoreParse = errors.New("evaluator score parse")

type EvaluatorScoreRequest struct {
	OrganizationUUID string
	WorkspaceUUID    string
	Model            string
	Prompt           string
}

type EvaluatorScore struct {
	Score       float64
	Explanation string
	Details     json.RawMessage
}

type EvaluatorModel interface {
	Score(context.Context, EvaluatorScoreRequest) (EvaluatorScore, error)
}

type LLMEvaluatorModel struct {
	database *db.DB
	secrets  *secrets.Service
	client   *http.Client
}

func NewLLMEvaluatorModel(database *db.DB, secretService *secrets.Service) *LLMEvaluatorModel {
	return &LLMEvaluatorModel{
		database: database,
		secrets:  secretService,
		client:   llmproviders.NewHTTPClient(30 * time.Second),
	}
}

func (m *LLMEvaluatorModel) Score(ctx context.Context, request EvaluatorScoreRequest) (EvaluatorScore, error) {
	if m == nil || m.database == nil || m.secrets == nil {
		return EvaluatorScore{}, errEvaluatorModelMissing
	}
	upstream, err := llmproviders.Resolve(ctx, m.database, m.secrets, request.OrganizationUUID, request.WorkspaceUUID, request.Model)
	if err != nil {
		if errors.Is(err, llmproviders.ErrNotConfigured) || errors.Is(err, llmproviders.ErrModelNotConfigured) {
			return EvaluatorScore{}, errEvaluatorModelMissing
		}
		return EvaluatorScore{}, err
	}
	return m.complete(ctx, upstream, request.Prompt)
}

func (m *LLMEvaluatorModel) complete(ctx context.Context, upstream llmproviders.Upstream, prompt string) (EvaluatorScore, error) {
	backend := upstream.APIBackend
	if backend == "" {
		backend = llmproviders.Messages
	}
	path := "/v1/messages"
	if backend == llmproviders.ChatCompletions {
		path = "/v1/chat/completions"
	}
	body, err := evaluatorScoreRequestBody(backend, upstream.ModelID, prompt)
	if err != nil {
		return EvaluatorScore{}, err
	}
	target, err := llmproviders.Endpoint(upstream.BaseURL, path, "")
	if err != nil {
		return EvaluatorScore{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return EvaluatorScore{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	upstream.ApplyHeaders(httpRequest.Header)
	client := m.client
	if client == nil {
		client = llmproviders.NewHTTPClient(30 * time.Second)
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return EvaluatorScore{}, errEvaluatorScoreFailed
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, evaluatorScoreBodyLimit))
	if err != nil {
		return EvaluatorScore{}, errEvaluatorScoreFailed
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return EvaluatorScore{}, errEvaluatorScoreFailed
	}
	text, err := evaluatorScoreText(backend, payload)
	if err != nil {
		return EvaluatorScore{}, err
	}
	return parseEvaluatorScoreText(text)
}

func evaluatorScoreRequestBody(backend, model, prompt string) ([]byte, error) {
	if backend == llmproviders.ChatCompletions {
		return json.Marshal(map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": evaluatorScoreSystemPrompt},
				{"role": "user", "content": prompt},
			},
		})
	}
	return json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": evaluatorScoreMaxTokens,
		"system":     evaluatorScoreSystemPrompt,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	})
}

func evaluatorScoreText(backend string, payload []byte) (string, error) {
	if backend == llmproviders.ChatCompletions {
		var body struct {
			Choices []struct {
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(payload, &body) != nil || len(body.Choices) == 0 {
			return "", errEvaluatorScoreParse
		}
		return contentText(body.Choices[0].Message.Content), nil
	}
	var body struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return "", errEvaluatorScoreParse
	}
	text := contentText(body.Content)
	if text == "" {
		return "", errEvaluatorScoreParse
	}
	return text, nil
}

func parseEvaluatorScoreText(text string) (EvaluatorScore, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	var body struct {
		Score       *float64        `json:"score"`
		Explanation string          `json:"explanation"`
		Details     json.RawMessage `json:"details"`
	}
	if json.Unmarshal([]byte(text), &body) != nil || body.Score == nil {
		return EvaluatorScore{}, errEvaluatorScoreParse
	}
	details, err := scoreDetailsObject(body.Details)
	if err != nil {
		return EvaluatorScore{}, err
	}
	return EvaluatorScore{Score: *body.Score, Explanation: body.Explanation, Details: details}, nil
}

func scoreDetailsObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil, errEvaluatorScoreParse
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, errEvaluatorScoreParse
	}
	return encoded, nil
}

var (
	errEvaluatorModelMissing = errors.New("evaluator model missing")
	errEvaluatorScoreFailed  = errors.New("evaluator score failed")
)
