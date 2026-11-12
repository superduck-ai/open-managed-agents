package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const omaManagedAgentsBeta = "managed-agents-2026-04-01"

type omaEvent struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Name            string          `json:"name"`
	Input           json.RawMessage `json:"input"`
	Permission      string          `json:"evaluated_permission"`
	ToolUseID       string          `json:"tool_use_id"`
	CustomToolUseID string          `json:"custom_tool_use_id"`
	SessionThreadID string          `json:"session_thread_id"`
	Content         json.RawMessage `json:"content"`
	StopReason      json.RawMessage `json:"stop_reason"`
	CreatedAt       string          `json:"created_at"`
}

type omaContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
	Name     string `json:"name"`
}

type omaSession struct {
	ID         string            `json:"id"`
	Status     string            `json:"status"`
	ArchivedAt *string           `json:"archived_at"`
	Metadata   map[string]string `json:"metadata"`
}

type omaClient struct {
	baseURL string
	http    *http.Client
}

func newOMAClient(baseURL string) *omaClient {
	return &omaClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *omaClient) createSession(ctx context.Context, apiKey, agentID, environmentID, title string, metadata map[string]string, resources []map[string]any, vaultIDs []string) (string, error) {
	body := map[string]any{
		"agent":          agentID,
		"environment_id": environmentID,
		"title":          title,
		"metadata":       metadata,
	}
	if len(resources) > 0 {
		body["resources"] = resources
	}
	if len(vaultIDs) > 0 {
		body["vault_ids"] = vaultIDs
	}
	var session omaSession
	if err := c.request(ctx, apiKey, http.MethodPost, "/v1/sessions", body, &session); err != nil {
		return "", err
	}
	if session.ID == "" {
		return "", errors.New("OMA 会话响应缺少 id")
	}
	return session.ID, nil
}

func (c *omaClient) getSession(ctx context.Context, apiKey, sessionID string) (omaSession, error) {
	var session omaSession
	err := c.request(ctx, apiKey, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID), nil, &session)
	return session, err
}

func (c *omaClient) sendUserMessage(ctx context.Context, apiKey, sessionID, text string) error {
	_, err := c.sendContent(ctx, apiKey, sessionID, []map[string]any{{"type": "text", "text": text}})
	return err
}

func (c *omaClient) sendContent(ctx context.Context, apiKey, sessionID string, content []map[string]any) ([]string, error) {
	body := map[string]any{
		"events": []map[string]any{{
			"type":    "user.message",
			"content": content,
		}},
	}
	var response struct {
		Data []omaEvent `json:"data"`
	}
	if err := c.request(ctx, apiKey, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", body, &response); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(response.Data))
	for _, event := range response.Data {
		if event.ID != "" {
			ids = append(ids, event.ID)
		}
	}
	return ids, nil
}

func (c *omaClient) interrupt(ctx context.Context, apiKey, sessionID string) error {
	body := map[string]any{"events": []map[string]any{{"type": "user.interrupt"}}}
	return c.request(ctx, apiKey, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", body, nil)
}

func (c *omaClient) sendEvent(ctx context.Context, apiKey, sessionID string, event map[string]any) error {
	body := map[string]any{"events": []map[string]any{event}}
	return c.request(ctx, apiKey, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", body, nil)
}

func (c *omaClient) listEvents(ctx context.Context, apiKey, sessionID string) ([]omaEvent, error) {
	var events []omaEvent
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/events?order=asc&limit=1000"
	for {
		var current struct {
			Data     []omaEvent `json:"data"`
			NextPage string     `json:"next_page"`
		}
		if err := c.request(ctx, apiKey, http.MethodGet, path, nil, &current); err != nil {
			return nil, err
		}
		events = append(events, current.Data...)
		if current.NextPage == "" || len(current.Data) == 0 {
			return events, nil
		}
		path = "/v1/sessions/" + url.PathEscape(sessionID) + "/events?order=asc&limit=1000&page=" + url.QueryEscape(current.NextPage)
	}
}

func (c *omaClient) request(ctx context.Context, apiKey, method, path string, body any, out any) error {
	if c.baseURL == "" {
		return errors.New("未配置 OMA 渠道内部地址")
	}
	endpoint := c.baseURL + path
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	endpoint += separator + "beta=true"
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return err
	}
	request.Header.Set("X-Api-Key", apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", omaManagedAgentsBeta)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("OMA 请求失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OMA 请求返回 HTTP %d", response.StatusCode)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("OMA 响应解析失败: %w", err)
	}
	return nil
}

func eventText(event omaEvent) string {
	if len(event.Content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(event.Content, &text); err == nil {
		return text
	}
	var blocks []omaContentBlock
	if err := json.Unmarshal(event.Content, &blocks); err != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func eventThinking(event omaEvent) string {
	var blocks []omaContentBlock
	if err := json.Unmarshal(event.Content, &blocks); err != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Thinking != "" {
			parts = append(parts, block.Thinking)
			continue
		}
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func eventToolName(event omaEvent) string {
	if event.Name != "" {
		return event.Name
	}
	var blocks []omaContentBlock
	if err := json.Unmarshal(event.Content, &blocks); err != nil {
		return ""
	}
	for _, block := range blocks {
		if block.Name != "" {
			return block.Name
		}
	}
	return ""
}

func stopReason(event omaEvent) string {
	if len(event.StopReason) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(event.StopReason, &value); err == nil {
		return value
	}
	var object struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(event.StopReason, &object); err != nil {
		return ""
	}
	return object.Type
}
