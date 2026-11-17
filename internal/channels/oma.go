package channels

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

const omaManagedAgentsBeta = "managed-agents-2026-04-01"

type omaEvent struct {
	ProcessedAt     json.RawMessage `json:"processed_at,omitempty"`
	Metadata        json.RawMessage `json:"metadata"`
	IsError         bool            `json:"is_error"`
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Name            string          `json:"name"`
	Input           json.RawMessage `json:"input"`
	Permission      string          `json:"evaluated_permission"`
	ToolUseID       string          `json:"tool_use_id"`
	CustomToolUseID string          `json:"custom_tool_use_id"`
	MCPToolUseID    string          `json:"mcp_tool_use_id"`
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

type omaSessionAgent struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type omaSessionResource struct {
	FileID        string `json:"file_id"`
	Type          string `json:"type"`
	MemoryStoreID string `json:"memory_store_id"`
	sessionresource.GitRepositorySpec
}

type omaSession struct {
	Resources     []omaSessionResource `json:"resources"`
	VaultIDs      []string             `json:"vault_ids"`
	Agent         omaSessionAgent      `json:"agent"`
	EnvironmentID string               `json:"environment_id"`
	ID            string               `json:"id"`
	Status        string               `json:"status"`
	ArchivedAt    *string              `json:"archived_at"`
	Metadata      map[string]string    `json:"metadata"`
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

func (c *omaClient) createSession(ctx context.Context, apiKey, agentID string, agentVersion int, environmentID, title string, metadata map[string]string, resources []map[string]any, vaultIDs []string) (string, error) {
	body := map[string]any{
		"agent":          agentID,
		"environment_id": environmentID,
		"title":          title,
		"metadata":       metadata,
	}
	if agentVersion > 0 {
		body["agent"] = omaSessionAgent{ID: agentID, Version: agentVersion}
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

func (c *omaClient) interruptAndWait(ctx context.Context, apiKey, sessionID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.interrupt(ctx, apiKey, sessionID); err != nil {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		session, err := c.getSession(ctx, apiKey, sessionID)
		if err != nil {
			return err
		}
		if session.Status == "idle" || session.Status == "terminated" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *omaClient) sendUserMessage(ctx context.Context, apiKey, sessionID, text string) error {
	_, err := c.sendContent(ctx, apiKey, sessionID, []map[string]any{{"type": "text", "text": text}}, nil)
	return err
}

func (c *omaClient) sendContent(ctx context.Context, apiKey, sessionID string, content []map[string]any, metadata *channelInputMetadata) ([]string, error) {
	input := map[string]any{"type": "user.message", "content": content}
	if metadata != nil {
		input["metadata"] = metadata
	}
	body := map[string]any{
		"events": []map[string]any{input},
	}
	var response struct {
		Data []omaEvent `json:"data"`
	}
	requestKey := ""
	if metadata != nil {
		requestKey = metadata.requestKey()
	}
	requestErr := c.requestWithIdempotency(ctx, apiKey, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", body, &response, requestKey)
	ids := make([]string, 0, len(response.Data))
	for _, event := range response.Data {
		if event.ID != "" {
			ids = append(ids, event.ID)
		}
	}
	if requestErr == nil && len(ids) > 0 || metadata == nil {
		return ids, requestErr
	}
	events, lookupErr := c.listEvents(ctx, apiKey, sessionID)
	var acceptedID string
	for _, event := range events {
		var association channelInputMetadata
		if event.Type != "user.message" || event.ID == "" || json.Unmarshal(event.Metadata, &association) != nil || !association.matches(*metadata) {
			continue
		}
		if acceptedID != "" {
			return nil, errors.Join(errChannelInputUnconfirmed, requestErr)
		}
		acceptedID = event.ID
	}
	if lookupErr != nil || acceptedID == "" {
		return nil, errors.Join(errChannelInputUnconfirmed, requestErr, lookupErr)
	}
	return []string{acceptedID}, nil
}

func (c *omaClient) interrupt(ctx context.Context, apiKey, sessionID string) error {
	body := map[string]any{"events": []map[string]any{{"type": "user.interrupt"}}}
	return c.request(ctx, apiKey, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", body, nil)
}

func (c *omaClient) sendEvent(ctx context.Context, apiKey, sessionID string, event map[string]any, requestKey string) error {
	body := map[string]any{"events": []map[string]any{event}}
	return c.requestWithIdempotency(ctx, apiKey, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/events", body, nil, requestKey)
}

func (c *omaClient) listEvents(ctx context.Context, apiKey, sessionID string) ([]omaEvent, error) {
	return c.listEventPages(ctx, apiKey, sessionID, nil)
}

func (c *omaClient) listEventPages(ctx context.Context, apiKey, sessionID string, filter url.Values) ([]omaEvent, error) {
	var events []omaEvent
	err := visitOMAEventPages(ctx, c, apiKey, sessionID, filter, func(page []omaEvent) error {
		events = append(events, page...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

func visitOMAEventPages[T any](ctx context.Context, c *omaClient, apiKey, sessionID string, filter url.Values, visit func([]T) error) error {
	pages := make(map[string]struct{})
	query := url.Values{"order": {"asc"}, "limit": {"1000"}}
	for key, values := range filter {
		query[key] = values
	}
	basePath := "/v1/sessions/" + url.PathEscape(sessionID) + "/events"
	for {
		var current struct {
			Data     []T    `json:"data"`
			NextPage string `json:"next_page"`
		}
		if err := c.request(ctx, apiKey, http.MethodGet, basePath+"?"+query.Encode(), nil, &current); err != nil {
			return err
		}
		if err := visit(current.Data); err != nil {
			return err
		}
		if current.NextPage == "" || len(current.Data) == 0 {
			return nil
		}
		if _, seen := pages[current.NextPage]; seen {
			return errOMAPaginationCycle
		}
		pages[current.NextPage] = struct{}{}
		query.Set("page", current.NextPage)
	}
}

func (c *omaClient) request(ctx context.Context, apiKey, method, path string, body any, out any) error {
	return c.requestWithIdempotency(ctx, apiKey, method, path, body, out, "")
}

func (c *omaClient) requestWithIdempotency(ctx context.Context, apiKey, method, path string, body any, out any, requestKey string) error {
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
	if requestKey != "" {
		request.Header.Set("Idempotency-Key", requestKey)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("OMA 请求失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return omaResponseError(response.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := jsonv2.UnmarshalRead(response.Body, out); err != nil && !errors.Is(err, io.EOF) {
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

type omaFile struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size_bytes"`
}

type omaResource = contracts.Resource

func (c *omaClient) downloadFile(ctx context.Context, apiKey, fileID string, expectedSize int64) ([]byte, string, error) {
	if c.baseURL == "" {
		return nil, "", errors.New("未配置 OMA 渠道内部地址")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/files/"+url.PathEscape(fileID)+"/content?beta=true", nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("X-Api-Key", apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", omaManagedAgentsBeta)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("OMA 下载文件失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("OMA 下载文件返回 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, expectedSize+1))
	if err != nil {
		return nil, "", err
	}
	return data, response.Header.Get("Content-Type"), nil
}

func (c *omaClient) fileMetadata(ctx context.Context, apiKey, fileID string) (omaFile, error) {
	var file omaFile
	if err := c.request(ctx, apiKey, http.MethodGet, "/v1/files/"+url.PathEscape(fileID), nil, &file); err != nil {
		return omaFile{}, err
	}
	return file, nil
}

func (c *omaClient) listResources(ctx context.Context, apiKey, sessionID string) ([]omaResource, error) {
	var response struct {
		Data []omaResource `json:"data"`
	}
	if err := c.request(ctx, apiKey, http.MethodGet, "/v1/sessions/"+url.PathEscape(sessionID)+"/resources", nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}
