package tunnels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
)

type IngressHandler struct {
	cfg          config.TunnelConfig
	db           *db.DB
	broker       *Broker
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

func NewIngressHandler(cfg config.TunnelConfig, database *db.DB, broker *Broker, logger *slog.Logger) *IngressHandler {
	if database == nil || broker == nil {
		panic("tunnels: ingress database and broker are required")
	}
	logger = logging.LoggerOrDefault(logger)
	handler := &IngressHandler{
		cfg: cfg, db: database, broker: broker,
		errorAdapter: httpapi.NewErrorAdapter(logger),
	}
	router := chi.NewRouter()
	wrap := handler.errorAdapter.Wrap
	router.NotFound(wrap(handler.ingressNotFound))
	router.MethodNotAllowed(wrap(handler.ingressNotFound))
	router.Route("/{tunnel_id}", func(r chi.Router) {
		r.Get("/", handler.getSSENotSupported)
		r.Post("/", wrap(handler.forwardMain))
		r.Delete("/", wrap(handler.terminateMain))
		r.Get("/{channel}", handler.getSSENotSupported)
		r.Post("/{channel}", wrap(handler.forwardChannel))
		r.Delete("/{channel}", wrap(handler.terminateChannel))
	})
	handler.router = router
	return handler
}

func (h *IngressHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *IngressHandler) ingressNotFound(http.ResponseWriter, *http.Request) error {
	return routeNotFound()
}

func (h *IngressHandler) getSSENotSupported(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST, DELETE")
	httpapi.WriteError(w, r, httpapi.NewError(
		http.StatusMethodNotAllowed,
		"invalid_request_error",
		"Standalone SSE streams are not supported for tunnel ingress",
	))
}

func (h *IngressHandler) forwardMain(w http.ResponseWriter, r *http.Request) error {
	return h.forwardDirect(w, r, "main", CommandTypeJSONRPC)
}

func (h *IngressHandler) forwardChannel(w http.ResponseWriter, r *http.Request) error {
	return h.forwardDirect(w, r, chi.URLParam(r, "channel"), CommandTypeJSONRPC)
}

func (h *IngressHandler) terminateMain(w http.ResponseWriter, r *http.Request) error {
	return h.forwardDirect(w, r, "main", CommandTypeSessionTermination)
}

func (h *IngressHandler) terminateChannel(w http.ResponseWriter, r *http.Request) error {
	return h.forwardDirect(w, r, chi.URLParam(r, "channel"), CommandTypeSessionTermination)
}

// HandleOAuthProtectedResource forwards RFC 9728 protected-resource discovery
// through the connector instead of exposing the private MCP network.
func (h *IngressHandler) HandleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.errorAdapter.Write(w, r, routeNotFound())
		return
	}
	channel := chi.URLParam(r, "channel")
	if channel == "" {
		channel = "main"
	}
	if err := h.forwardDirect(w, r, channel, CommandTypeOAuthDiscovery); err != nil {
		h.errorAdapter.Write(w, r, err)
	}
}

func (h *IngressHandler) forwardDirect(w http.ResponseWriter, r *http.Request, channel string, commandType CommandType) error {
	principal, err := requireDirectIngressAPIKey(r)
	if err != nil {
		return err
	}
	return h.forwardScoped(w, r, principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "tunnel_id"), channel, commandType)
}

func (h *IngressHandler) forwardScoped(
	w http.ResponseWriter,
	r *http.Request,
	organizationUUID string,
	workspaceUUID string,
	tunnelID string,
	channel string,
	commandType CommandType,
) error {
	if !channelNamePattern.MatchString(channel) {
		return invalidRequest(errors.New("channel is invalid"))
	}
	tunnel, err := h.db.GetMCPTunnel(r.Context(), organizationUUID, workspaceUUID, tunnelID)
	if err != nil {
		return mapTunnelLookupError(err, tunnelID, "retrieve")
	}
	return h.forwardTunnel(w, r, tunnel, channel, commandType)
}

func (h *IngressHandler) forwardTunnel(
	w http.ResponseWriter,
	r *http.Request,
	tunnel db.MCPTunnel,
	channel string,
	commandType CommandType,
) error {
	if !channelNamePattern.MatchString(channel) {
		return invalidRequest(errors.New("channel is invalid"))
	}
	if tunnel.ArchivedAt != nil {
		return tunnelNotFound(tunnel.ExternalID, db.ErrNotFound)
	}
	body, err := h.readCommandBody(w, r, commandType)
	if err != nil {
		return err
	}
	headers, headerBytes, err := sanitizeIngressHeaders(r.Header, h.cfg)
	if err != nil {
		return invalidRequest(err)
	}
	if commandType == CommandTypeOAuthDiscovery {
		headers = make(http.Header)
		headerBytes = 0
	}
	requestID, err := ids.New("req_")
	if err != nil {
		return internalError("Could not generate tunnel request ID", fmt.Errorf("generate tunnel request ID: %w", err))
	}
	now := time.Now().UTC()
	deadline := now.Add(h.cfg.RequestTimeout)
	command := queuedCommand{
		RequestID: requestID, CommandType: commandType, Channel: channel,
		CreatedAt: now, Headers: headers, ExpiresAt: deadline,
		PayloadSize: int64(len(body)) + headerBytes,
		AffinityKey: headers.Get("Mcp-Session-Id"),
	}
	if commandType == CommandTypeJSONRPC {
		command.JSONRPC = body
	}
	waiter, err := h.broker.subscribeResponse(r.Context(), tunnel.UUID, requestID, true)
	if err != nil {
		return ingressQueueError(err)
	}
	defer waiter.Close()
	if err := h.broker.Enqueue(r.Context(), tunnel.UUID, command); err != nil {
		return ingressQueueError(err)
	}
	wroteStream := false
	requestCtx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	response, err := waiter.Wait(requestCtx, func(notification TunnelResponse) {
		if len(notification.JSONResponse) == 0 {
			return
		}
		if !wroteStream {
			writeIngressHeaders(w.Header(), notification.ResponseHeaders, canonicalTunnelOAuthMetadataURL(r, h.cfg, tunnel.ExternalID, channel))
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			status := notification.ResponseCode
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			wroteStream = true
		}
		writeSSEMessage(w, notification.JSONResponse)
	})
	if err != nil {
		if shouldCancelTunnelRequest(err) {
			h.cancelDisconnectedRequest(tunnel.UUID, requestID)
		}
		if wroteStream {
			return nil
		}
		return ingressResponseError(err)
	}
	if wroteStream {
		if len(response.JSONResponse) > 0 {
			writeSSEMessage(w, response.JSONResponse)
		}
		return nil
	}
	publicMCPURL := canonicalTunnelMCPURL(r, h.cfg, tunnel.ExternalID, channel)
	if commandType == CommandTypeOAuthDiscovery {
		return writeOAuthDiscoveryResponse(w, response, publicMCPURL)
	}
	writeIngressResponse(w, response, canonicalTunnelOAuthMetadataURL(r, h.cfg, tunnel.ExternalID, channel))
	return nil
}

func shouldCancelTunnelRequest(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// ServeTunnel implements the Code Session in-process TunnelInvoker boundary.
// It only claims canonical Tunnel paths or the configured Tunnel hostname suffix;
// ordinary MCP URLs remain on the existing outbound proxy path.
func (h *IngressHandler) ServeTunnel(
	w http.ResponseWriter,
	r *http.Request,
	organizationUUID string,
	workspaceUUID string,
	target *url.URL,
) bool {
	tunnel, channel, recognized, err := h.resolveTunnelTarget(r.Context(), organizationUUID, workspaceUUID, target)
	if !recognized {
		return false
	}
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return true
	}
	if r.Method == http.MethodGet {
		h.getSSENotSupported(w, r)
		return true
	}
	commandType := CommandTypeJSONRPC
	if r.Method == http.MethodDelete {
		commandType = CommandTypeSessionTermination
	} else if r.Method != http.MethodPost {
		h.errorAdapter.Write(w, r, invalidRequest(errors.New("tunnel ingress only supports GET, POST, and DELETE")))
		return true
	}
	if err := h.forwardTunnel(w, r, tunnel, channel, commandType); err != nil {
		h.errorAdapter.Write(w, r, err)
	}
	return true
}

// ServeTunnelOAuthDiscovery resolves the original configured Tunnel target,
// then uses the connector's dedicated OAuth discovery command.
func (h *IngressHandler) ServeTunnelOAuthDiscovery(
	w http.ResponseWriter,
	r *http.Request,
	organizationUUID string,
	workspaceUUID string,
	target *url.URL,
) bool {
	tunnel, channel, recognized, err := h.resolveTunnelTarget(r.Context(), organizationUUID, workspaceUUID, target)
	if !recognized {
		return false
	}
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return true
	}
	if err := h.forwardTunnel(w, r, tunnel, channel, CommandTypeOAuthDiscovery); err != nil {
		h.errorAdapter.Write(w, r, err)
	}
	return true
}

func (h *IngressHandler) resolveTunnelTarget(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	target *url.URL,
) (db.MCPTunnel, string, bool, error) {
	reference, recognized, err := RecognizeTarget(target, h.cfg)
	if !recognized {
		return db.MCPTunnel{}, "", false, nil
	}
	if err != nil {
		return db.MCPTunnel{}, "", true, invalidRequest(err)
	}
	var tunnel db.MCPTunnel
	if reference.Domain != "" {
		tunnel, err = h.db.GetMCPTunnelByDomain(ctx, organizationUUID, workspaceUUID, reference.Domain)
	} else {
		tunnel, err = h.db.GetMCPTunnel(ctx, organizationUUID, workspaceUUID, reference.TunnelID)
	}
	if err != nil {
		identifier := reference.TunnelID
		if reference.Domain != "" {
			identifier = reference.Domain
		}
		return db.MCPTunnel{}, "", true, mapTunnelLookupError(err, identifier, "retrieve")
	}
	return tunnel, reference.Channel, true, nil
}

func (h *IngressHandler) readCommandBody(w http.ResponseWriter, r *http.Request, commandType CommandType) (json.RawMessage, error) {
	if commandType == CommandTypeSessionTermination || commandType == CommandTypeOAuthDiscovery {
		return nil, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apperrRequestTooLarge("Tunnel request body exceeds the configured limit", err)
		}
		return nil, invalidRequest(errors.New("Invalid request body"))
	}
	if len(body) == 0 || !json.Valid(body) {
		return nil, invalidRequest(errors.New("request body must contain valid JSON"))
	}
	return json.RawMessage(body), nil
}

func requireDirectIngressAPIKey(r *http.Request) (auth.Principal, error) {
	if strings.TrimSpace(r.Header.Get("X-Api-Key")) == "" {
		return auth.Principal{}, missingAPIKey()
	}
	return requireAPIKey(r)
}

func sanitizeIngressHeaders(source http.Header, cfg config.TunnelConfig) (http.Header, int64, error) {
	connectionHeaders := make(map[string]struct{})
	for _, value := range source.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			connectionHeaders[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
	}
	denied := map[string]struct{}{
		"connection": {}, "keep-alive": {}, "proxy-authenticate": {}, "proxy-authorization": {},
		"te": {}, "trailer": {}, "transfer-encoding": {}, "upgrade": {}, "cookie": {}, "content-length": {},
		"x-api-key": {}, "x-tunnel-shard-token": {}, "x-tunnel-client-instance-id": {},
		"x-tunnel-mcp-server-info": {},
	}
	result := make(http.Header)
	var total int64
	for name, values := range source {
		normalized := strings.ToLower(name)
		if _, blocked := denied[normalized]; blocked {
			continue
		}
		if _, nominated := connectionHeaders[normalized]; nominated {
			continue
		}
		total += int64(len(name))
		for _, value := range values {
			if int64(len(value)) > cfg.MaxHeaderValueBytes {
				return nil, 0, errors.New("request header value exceeds the configured limit")
			}
			total += int64(len(value))
			result.Add(name, value)
		}
	}
	if total > cfg.MaxHeaderBytes {
		return nil, 0, errors.New("request headers exceed the configured limit")
	}
	return result, total, nil
}

func writeIngressResponse(w http.ResponseWriter, response TunnelResponse, oauthMetadataURL string) {
	writeIngressHeaders(w.Header(), response.ResponseHeaders, oauthMetadataURL)
	eventStream := strings.EqualFold(strings.TrimSpace(strings.SplitN(w.Header().Get("Content-Type"), ";", 2)[0]), "text/event-stream")
	if eventStream {
		w.Header().Set("Cache-Control", "no-cache")
	}
	status := response.ResponseCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(response.JSONResponse) > 0 {
		if eventStream {
			writeSSEMessage(w, response.JSONResponse)
		} else {
			_, _ = w.Write(response.JSONResponse)
		}
	}
}

func writeIngressHeaders(destination, source http.Header, oauthMetadataURL string) {
	for name, values := range source {
		for _, value := range values {
			if strings.EqualFold(name, "WWW-Authenticate") {
				value = rewriteResourceMetadata(value, oauthMetadataURL)
			}
			destination.Add(name, value)
		}
	}
}

func writeOAuthDiscoveryResponse(w http.ResponseWriter, response TunnelResponse, publicMCPURL string) error {
	var body map[string]any
	if len(response.JSONResponse) > 0 {
		if err := json.Unmarshal(response.JSONResponse, &body); err != nil || body == nil {
			return unavailable("Tunnel OAuth discovery returned invalid metadata", errors.New("OAuth metadata must be a JSON object"))
		}
	}
	if body == nil {
		body = map[string]any{}
	}
	body["resource"] = publicMCPURL
	writeIngressHeaders(w.Header(), response.ResponseHeaders, "")
	w.Header().Set("Content-Type", "application/json")
	status := response.ResponseCode
	if status == 0 {
		status = http.StatusOK
	}
	httpapi.WriteJSON(w, status, body)
	return nil
}

func rewriteResourceMetadata(value, oauthMetadataURL string) string {
	if oauthMetadataURL == "" {
		return value
	}
	for _, quote := range []string{"\"", ""} {
		prefix := "resource_metadata=" + quote
		start := strings.Index(value, prefix)
		if start == -1 {
			continue
		}
		valueStart := start + len(prefix)
		valueEnd := len(value)
		if quote != "" {
			if end := strings.Index(value[valueStart:], quote); end >= 0 {
				valueEnd = valueStart + end
			}
		} else if end := strings.IndexAny(value[valueStart:], ", "); end >= 0 {
			valueEnd = valueStart + end
		}
		return value[:valueStart] + oauthMetadataURL + value[valueEnd:]
	}
	return value
}

func writeSSEMessage(w http.ResponseWriter, payload json.RawMessage) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err == nil {
		payload = compact.Bytes()
	}
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (h *IngressHandler) cancelDisconnectedRequest(tunnelUUID, requestID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = h.broker.Cancel(ctx, tunnelUUID, requestID)
}
