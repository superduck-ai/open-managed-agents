package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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

type omaFile struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size_bytes"`
}

type omaResource struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	FileID    string `json:"file_id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	MountPath string `json:"mount_path"`
}

func (c *omaClient) uploadFile(ctx context.Context, apiKey, filename, mediaType string, data []byte) (omaFile, error) {
	if c.baseURL == "" {
		return omaFile{}, errors.New("未配置 OMA 渠道内部地址")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return omaFile{}, err
	}
	if _, err := part.Write(data); err != nil {
		return omaFile{}, err
	}
	if err := writer.Close(); err != nil {
		return omaFile{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/files?beta=true", &body)
	if err != nil {
		return omaFile{}, err
	}
	request.Header.Set("X-Api-Key", apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", omaManagedAgentsBeta)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := c.http.Do(request)
	if err != nil {
		return omaFile{}, fmt.Errorf("OMA 上传文件失败: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return omaFile{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return omaFile{}, fmt.Errorf("OMA 上传文件返回 HTTP %d", response.StatusCode)
	}
	var file omaFile
	if err := json.Unmarshal(payload, &file); err != nil {
		return omaFile{}, fmt.Errorf("OMA 文件响应解析失败: %w", err)
	}
	if file.ID == "" {
		return omaFile{}, errors.New("OMA 文件响应缺少 id")
	}
	if file.MimeType == "" {
		file.MimeType = mediaType
	}
	return file, nil
}

func (c *omaClient) downloadFile(ctx context.Context, apiKey, fileID string) ([]byte, string, error) {
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
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
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

func (c *omaClient) attachFileResource(ctx context.Context, apiKey, sessionID, fileID string) (omaResource, error) {
	body := map[string]any{"type": "file", "file_id": fileID}
	var resource omaResource
	if err := c.request(ctx, apiKey, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/resources", body, &resource); err != nil {
		return omaResource{}, err
	}
	return resource, nil
}
