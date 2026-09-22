package environments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// buildJobRef is an opaque, persistable locator for one remote build job.
type buildJobRef string

type buildJobStatus struct {
	State   string // queued, running, succeeded, failed, cancelled
	Message string
}

type imageBuildInput struct{ Dockerfile, Repository, Tag string }
type imageBuildStatus struct {
	buildJobStatus
	ImageRef string
}
type templateBuildStatus struct {
	buildJobStatus
	TemplateID string
}

type buildLogChunk struct {
	Text       string `json:"text"`
	NextCursor string `json:"next_cursor"`
	Complete   bool   `json:"complete"`
}

type buildCapabilities struct{ Logs, Cancel bool }

type imageBuilder interface {
	Capabilities() buildCapabilities
	Start(context.Context, imageBuildInput) (buildJobRef, error)
	GetStatus(context.Context, buildJobRef) (imageBuildStatus, error)
	ReadLogs(context.Context, buildJobRef, string) (buildLogChunk, error)
	Cancel(context.Context, buildJobRef) error
}

type templateBuilder interface {
	Capabilities() buildCapabilities
	Start(context.Context, string) (buildJobRef, error)
	GetStatus(context.Context, buildJobRef) (templateBuildStatus, error)
	ReadLogs(context.Context, buildJobRef, string) (buildLogChunk, error)
	Cancel(context.Context, buildJobRef) error
}

type buildHTTP struct {
	client        *http.Client
	header, token string
}

func newBuildHTTP(header, token string) buildHTTP {
	return buildHTTP{client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, header: header, token: token}
}

func (c buildHTTP) call(ctx context.Context, method, endpoint string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("create build request: %w", err)
	}
	req.Header.Set(c.header, c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("build provider request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("build provider returned HTTP %d", resp.StatusCode)
	}
	if output == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("build provider response exceeds 8 MiB")
	}
	if raw, ok := output.(*[]byte); ok {
		*raw = data
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("invalid build provider response: %w", err)
	}
	return nil
}

func encodeJobRef(value any) buildJobRef { data, _ := json.Marshal(value); return buildJobRef(data) }
