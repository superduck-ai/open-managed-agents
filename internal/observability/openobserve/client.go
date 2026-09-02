package openobserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

const maxSearchResponseBytes = 8 << 20

type searchClient struct {
	baseURL      string
	organization string
	username     string
	password     string
	http         *http.Client
	logger       *slog.Logger
}

type searchRequest struct {
	Query struct {
		SQL       string `json:"sql"`
		StartTime int64  `json:"start_time"`
		EndTime   int64  `json:"end_time"`
		From      int    `json:"from"`
		Size      int    `json:"size"`
	} `json:"query"`
}

type searchResponse struct {
	Hits []map[string]any `json:"hits"`
}

func newSearchClient(cfg config.OpenObserveConfig, logger *slog.Logger, httpClient *http.Client) *searchClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Query.Timeout}
	}
	return &searchClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		organization: strings.TrimSpace(cfg.Organization),
		username:     strings.TrimSpace(cfg.Query.Username),
		password:     cfg.Query.Password,
		http:         httpClient,
		logger:       logging.LoggerOrDefault(logger),
	}
}

func (c *searchClient) search(ctx context.Context, signal streamType, sql string, start, end time.Time, size int) ([]map[string]any, error) {
	var body searchRequest
	body.Query.SQL = sql
	body.Query.StartTime = start.UnixMicro()
	body.Query.EndTime = end.UnixMicro()
	body.Query.Size = size
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, observability.QueryInternal("encode observability search request", err)
	}
	endpoint := c.baseURL + "/api/" + url.PathEscape(c.organization) + "/_search?type=" + url.QueryEscape(string(signal))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, observability.QueryInternal("create observability search request", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(c.username, c.password)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, mapSearchTransportError(err)
	}
	defer func() { _ = response.Body.Close() }()
	limited, err := io.ReadAll(io.LimitReader(response.Body, maxSearchResponseBytes+1))
	if err != nil {
		return nil, observability.QueryUnavailable(err)
	}
	if int64(len(limited)) > maxSearchResponseBytes {
		c.logger.WarnContext(ctx, "observability search response exceeded size limit", "status", response.StatusCode, "body_bytes", len(limited))
		return nil, observability.QueryInternal("observability search response exceeded size limit", nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		c.logger.WarnContext(ctx, "observability search returned a non-success status", "status", response.StatusCode, "body_bytes", len(limited))
		if response.StatusCode >= 500 {
			return nil, observability.QueryUnavailable(fmt.Errorf("observability search status %d", response.StatusCode))
		}
		return nil, observability.QueryInternal("observability search failed", fmt.Errorf("status %d", response.StatusCode))
	}
	var decoded searchResponse
	if err := json.Unmarshal(limited, &decoded); err != nil {
		return nil, observability.QueryInternal("decode observability search response", err)
	}
	if decoded.Hits == nil {
		return []map[string]any{}, nil
	}
	return decoded.Hits, nil
}

func mapSearchTransportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return observability.QueryTimeout(err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return observability.QueryTimeout(err)
	}
	return observability.QueryUnavailable(err)
}
