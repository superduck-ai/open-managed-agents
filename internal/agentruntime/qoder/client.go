package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	DefaultEndpoint        = "https://api.qoder.com.cn/api/v1/cloud"
	defaultRESTTimeout     = 30 * time.Second
	defaultDialTimeout     = 10 * time.Second
	defaultHeaderTimeout   = 30 * time.Second
	defaultHubIdleTimeout  = 15 * time.Second
	defaultOSSStorageHost  = "cloud-agents-storage-cn.oss-cn-beijing.aliyuncs.com"
	defaultOSSRegionHost   = "*.oss-cn-*.aliyuncs.com"
	maxJSONResponseBytes   = 16 * 1024 * 1024
	watermarkOrgKey        = "oma_org_uuid"
	watermarkWorkspaceKey  = "oma_workspace_id"
	maxQoderEventPageLimit = 100
)

type UsageRecorder interface {
	RecordProviderUsage(ctx context.Context, record db.ProviderUsageRecord) (db.ProviderUsageRecord, error)
}

var (
	_ UsageRecorder                    = (*db.DB)(nil)
	_ agentruntime.Runtime             = (*Runtime)(nil)
	_ agentruntime.ModelRuntime        = (*Runtime)(nil)
	_ agentruntime.AgentRuntime        = (*Runtime)(nil)
	_ agentruntime.AgentVersionRuntime = (*Runtime)(nil)
	_ agentruntime.EnvironmentRuntime  = (*Runtime)(nil)
	_ agentruntime.FileRuntime         = (*Runtime)(nil)
	_ agentruntime.SessionRuntime      = (*Runtime)(nil)
	_ agentruntime.HealthChecker       = (*Runtime)(nil)
)

type profileConfig struct {
	AllowedDownloadHosts []string                            `json:"allowed_download_hosts"`
	FileConstraints      *agentruntime.FileUploadConstraints `json:"file_constraints"`
}

type credentialsPayload struct {
	Token  string `json:"token"`
	APIKey string `json:"api_key"`
}

type Runtime struct {
	profile              db.RuntimeProfile
	endpoint             string
	token                string
	caps                 agentruntime.CapabilitySet
	bindings             *agentruntime.BindingService
	usageStore           UsageRecorder
	logger               *slog.Logger
	restClient           *http.Client
	streamClient         *http.Client
	downloadClient       *http.Client
	allowedDownloadHosts []string
	allowDevFakeIP       bool
	lookupIP             func(ctx context.Context, host string) ([]net.IP, error)
	hub                  *streamHub
	usageMu              sync.Mutex
	lastCredits          map[string]float64
}

type FactoryOption func(*factoryOptions)

type factoryOptions struct {
	allowDevFakeIP bool
}

func WithAllowDevFakeIP(allow bool) FactoryOption {
	return func(o *factoryOptions) {
		o.allowDevFakeIP = allow
	}
}

func NewFactory(bindings *agentruntime.BindingService, usageStore UsageRecorder, logger *slog.Logger, opts ...FactoryOption) agentruntime.Factory {
	baseLogger := logging.LoggerOrDefault(logger)
	var applied factoryOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return func(_ context.Context, cfg agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return newRuntime(cfg, bindings, usageStore, baseLogger, applied)
	}
}

func newRuntime(
	cfg agentruntime.ProfileRuntimeConfig,
	bindings *agentruntime.BindingService,
	usageStore UsageRecorder,
	logger *slog.Logger,
	opts factoryOptions,
) (*Runtime, error) {
	token, err := parseCredentials(cfg.Credentials)
	if err != nil {
		return nil, err
	}
	endpoint, err := normalizeEndpoint(cfg.Profile.EndpointURL)
	if err != nil {
		return nil, err
	}
	parsedCfg, err := parseProfileConfig(cfg.Profile.Config)
	if err != nil {
		return nil, err
	}
	caps := cfg.Capabilities
	if caps.UsageMetering == "" {
		caps = agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
	}
	if parsedCfg.FileConstraints != nil {
		if parsedCfg.FileConstraints.MaxSizeBytes < 0 {
			return nil, agentruntime.InvalidArgumentError("file_constraints.max_size_bytes must be non-negative", nil)
		}
		caps.FileConstraints = *parsedCfg.FileConstraints
	}
	allowedHosts := buildAllowedDownloadHosts(parsedCfg.AllowedDownloadHosts)

	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	restTransport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   defaultDialTimeout,
		ResponseHeaderTimeout: defaultHeaderTimeout,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
	}
	streamTransport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   defaultDialTimeout,
		ResponseHeaderTimeout: defaultHeaderTimeout,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
	}

	rt := &Runtime{
		profile:              cfg.Profile,
		endpoint:             endpoint,
		token:                token,
		caps:                 caps,
		bindings:             bindings,
		usageStore:           usageStore,
		logger:               logging.LoggerOrDefault(logger).With("component", "agentruntime.qoder", "profile_uuid", cfg.Profile.UUID),
		restClient:           &http.Client{Timeout: defaultRESTTimeout, Transport: restTransport},
		streamClient:         &http.Client{Transport: streamTransport},
		allowedDownloadHosts: allowedHosts,
		allowDevFakeIP:       opts.allowDevFakeIP,
		lookupIP:             defaultLookupIP,
		lastCredits:          make(map[string]float64),
	}
	rt.downloadClient = rt.newDownloadClient(dialer)
	rt.hub = newStreamHub(rt, defaultHubIdleTimeout)
	return rt, nil
}

func defaultLookupIP(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

func parseCredentials(raw []byte) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", invalidCredentialsError("qoder runtime credentials are required", nil)
	}
	if trimmed[0] == '{' {
		var payload credentialsPayload
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return "", invalidCredentialsError("qoder runtime credentials JSON is invalid", err)
		}
		token := strings.TrimSpace(payload.Token)
		if token == "" {
			token = strings.TrimSpace(payload.APIKey)
		}
		if token == "" {
			return "", invalidCredentialsError("qoder runtime credentials require token or api_key", nil)
		}
		return token, nil
	}
	token := strings.TrimSpace(string(trimmed))
	if token == "" {
		return "", invalidCredentialsError("qoder runtime credentials are empty", nil)
	}
	return token, nil
}

func normalizeEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultEndpoint, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", invalidEndpointError("qoder runtime endpoint is invalid", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", invalidEndpointError("qoder runtime endpoint must use http or https", nil)
	}
	if parsed.Hostname() == "" {
		return "", invalidEndpointError("qoder runtime endpoint host is required", nil)
	}
	return strings.TrimRight(trimmed, "/"), nil
}

func parseProfileConfig(raw json.RawMessage) (profileConfig, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return profileConfig{}, nil
	}
	var parsed profileConfig
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return profileConfig{}, agentruntime.InvalidArgumentError("invalid qoder profile config", err)
	}
	return parsed, nil
}

func buildAllowedDownloadHosts(extra []string) []string {
	hosts := []string{defaultOSSStorageHost, defaultOSSRegionHost}
	seen := map[string]struct{}{
		defaultOSSStorageHost: {},
		defaultOSSRegionHost:  {},
	}
	for _, host := range extra {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		hosts = append(hosts, normalized)
	}
	return hosts
}

func (r *Runtime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (r *Runtime) Capabilities() agentruntime.CapabilitySet {
	return r.caps
}

func (r *Runtime) Close() error {
	if r.hub != nil {
		r.hub.close()
	}
	if r.restClient != nil {
		r.restClient.CloseIdleConnections()
	}
	if r.streamClient != nil {
		r.streamClient.CloseIdleConnections()
	}
	if r.downloadClient != nil {
		r.downloadClient.CloseIdleConnections()
	}
	return nil
}

func (r *Runtime) HealthCheck(ctx context.Context) (agentruntime.HealthCheckResult, error) {
	started := time.Now().UTC()
	models, err := r.fetchModels(ctx)
	checkedAt := time.Now().UTC()
	latency := checkedAt.Sub(started)
	if err != nil {
		return agentruntime.HealthCheckResult{
			Status:    "unhealthy",
			Latency:   latency,
			Message:   err.Error(),
			CheckedAt: checkedAt,
		}, err
	}
	if len(models) == 0 {
		healthErr := unhealthyError("qoder runtime returned no enabled models", nil)
		return agentruntime.HealthCheckResult{
			Status:    "unhealthy",
			Latency:   latency,
			Message:   "no enabled models available",
			CheckedAt: checkedAt,
		}, healthErr
	}
	return agentruntime.HealthCheckResult{
		Status:    "healthy",
		Latency:   latency,
		Message:   "qoder runtime ready",
		CheckedAt: checkedAt,
	}, nil
}

func (r *Runtime) buildURL(path string, query url.Values) string {
	full := r.endpoint + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	return full
}

func (r *Runtime) doJSON(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	headers map[string]string,
	reqBody any,
	respOut any,
) error {
	var bodyReader io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return agentruntime.InvalidArgumentError("failed to encode qoder request payload", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.buildURL(path, query), bodyReader)
	if err != nil {
		return upstreamTransportError(err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := r.restClient.Do(req)
	if err != nil {
		r.logger.WarnContext(ctx, "qoder upstream request failed", "method", method, "path", path, "error", err)
		return upstreamTransportError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		mapped := readAndMapUpstreamError(resp)
		r.logger.WarnContext(ctx, "qoder upstream non-2xx response", "method", method, "path", path, "status", resp.StatusCode)
		return mapped
	}
	if respOut == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseBytes))
	if err != nil {
		return upstreamTransportError(err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, respOut); err != nil {
		return upstreamTransportError(err)
	}
	return nil
}

func injectWatermark(metadata map[string]string, scope agentruntime.Scope) map[string]string {
	merged := make(map[string]string, len(metadata)+2)
	for k, v := range metadata {
		merged[k] = v
	}
	merged[watermarkOrgKey] = scope.OrganizationUUID
	merged[watermarkWorkspaceKey] = scope.WorkspaceUUID
	return merged
}

func stripWatermark(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cleaned := make(map[string]string, len(metadata))
	for k, v := range metadata {
		if k == watermarkOrgKey || k == watermarkWorkspaceKey {
			continue
		}
		cleaned[k] = v
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func normalizeListLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func normalizeListOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
