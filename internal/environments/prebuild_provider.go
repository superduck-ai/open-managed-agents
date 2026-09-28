package environments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// buildJobRef is an opaque, persistable locator for one remote build job.
type buildJobRef string

// buildProviderError is a completed build provider response that rejected the
// request. Callers must not treat every provider failure alike: a client-side
// rejection proves that no remote work started, while transport loss,
// redirects and server-side failures leave the remote outcome unknown.
type buildProviderError struct {
	Status int
}

func (e *buildProviderError) Error() string {
	return fmt.Sprintf("build provider returned HTTP %d", e.Status)
}

// providerRejectionStatus reports the status code of a response that
// definitively rejected the submission without starting remote work. 4xx
// responses are decided before the provider acts on the request; 5xx responses
// may follow partial work, so they stay ambiguous.
func providerRejectionStatus(err error) (int, bool) {
	var providerError *buildProviderError
	if errors.As(err, &providerError) && providerError.Status >= 400 && providerError.Status < 500 {
		return providerError.Status, true
	}
	return 0, false
}

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
		return &buildProviderError{Status: resp.StatusCode}
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
