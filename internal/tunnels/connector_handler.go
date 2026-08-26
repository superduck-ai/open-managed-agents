package tunnels

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
)

const (
	connectorInstanceHeader = "X-Tunnel-Client-Instance-Id"
	serverInfoHeader        = "X-Tunnel-MCP-Server-Info"
	shardTokenHeader        = "X-Tunnel-Shard-Token"
	defaultPollLimit        = 25
	maxPollLimit            = 25
	maxConnectorInstanceID  = 128
	maxShardTokenBytes      = 256
)

type ConnectorHandler struct {
	cfg          config.TunnelConfig
	db           connectorDatabase
	broker       *Broker
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

type connectorDatabase interface {
	FindMCPTunnelTokenContext(context.Context, string, []byte) (db.MCPTunnelTokenContext, error)
	GetMCPTunnel(context.Context, string, string, string) (db.MCPTunnel, error)
}

type connectorAuthContext struct {
	TunnelUUID       string
	TunnelExternalID string
	OrganizationUUID string
	WorkspaceUUID    string
	TokenVersion     int64
}

type connectorTunnelMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type polledCommandEnvelope struct {
	Commands []json.RawMessage `json:"commands"`
}

func NewConnectorHandler(cfg config.TunnelConfig, database *db.DB, broker *Broker, logger *slog.Logger) *ConnectorHandler {
	if database == nil || broker == nil {
		panic("tunnels: connector database and broker are required")
	}
	logger = logging.LoggerOrDefault(logger)
	handler := &ConnectorHandler{
		cfg: cfg, db: database, broker: broker,
		errorAdapter: httpapi.NewErrorAdapter(logger),
	}
	router := chi.NewRouter()
	wrap := handler.errorAdapter.Wrap
	router.NotFound(wrap(handler.connectorNotFound))
	router.MethodNotAllowed(wrap(handler.connectorNotFound))
	router.Route("/v1/tunnels/{tunnel_id}", func(r chi.Router) {
		r.Get("/", wrap(handler.metadata))
		r.Get("/poll", wrap(handler.poll))
		r.Post("/response", wrap(handler.postResponse))
	})
	handler.router = router
	return handler
}

func (h *ConnectorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *ConnectorHandler) connectorNotFound(http.ResponseWriter, *http.Request) error {
	return routeNotFound()
}

func (h *ConnectorHandler) metadata(w http.ResponseWriter, r *http.Request) error {
	credential, err := h.authenticate(r, false)
	if err != nil {
		return err
	}
	tunnel, err := h.db.GetMCPTunnel(
		r.Context(), credential.OrganizationUUID, credential.WorkspaceUUID, credential.TunnelExternalID,
	)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return invalidConnectorCredential()
		}
		return internalError("Could not load tunnel metadata", fmt.Errorf("load tunnel metadata: %w", err))
	}
	if tunnel.ArchivedAt != nil {
		return invalidConnectorCredential()
	}
	name := tunnel.ExternalID
	if tunnel.DisplayName != nil && *tunnel.DisplayName != "" {
		name = *tunnel.DisplayName
	}
	httpapi.WriteJSON(w, http.StatusOK, connectorTunnelMetadata{
		ID: tunnel.ExternalID, Name: name, Description: "",
	})
	return nil
}

func (h *ConnectorHandler) poll(w http.ResponseWriter, r *http.Request) error {
	credential, err := h.authenticate(r, false)
	if err != nil {
		return err
	}
	channels, err := ParseMCPServerInfo(r.Header.Get(serverInfoHeader))
	if err != nil {
		return invalidRequest(err)
	}
	channels, err = filterPollChannels(channels, r.URL.Query()["channel"])
	if err != nil {
		return invalidRequest(err)
	}
	limit, timeout, err := h.pollOptions(r)
	if err != nil {
		return invalidRequest(err)
	}
	instanceID, err := connectorInstanceID(r, channelsRequireProcessAffinity(channels))
	if err != nil {
		return invalidRequest(err)
	}
	commands, err := h.broker.Poll(r.Context(), credential.TunnelUUID, instanceID, credential.TokenVersion, channels, limit, timeout)
	if err != nil {
		if errors.Is(err, ErrTokenRetired) {
			return invalidConnectorCredential()
		}
		if errors.Is(err, ErrChannelMismatch) || errors.Is(err, ErrChannelLimit) || errors.Is(err, ErrChannelInvalid) {
			return invalidRequest(err)
		}
		return unavailable("Tunnel broker is unavailable", err)
	}
	if len(commands) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	wireCommands := make([]json.RawMessage, 0, len(commands))
	for _, command := range commands {
		wire, err := command.MarshalWireJSON()
		if err != nil {
			return internalError("Could not encode tunnel command", err)
		}
		wireCommands = append(wireCommands, wire)
	}
	httpapi.WriteJSON(w, http.StatusOK, polledCommandEnvelope{Commands: wireCommands})
	return nil
}

func (h *ConnectorHandler) postResponse(w http.ResponseWriter, r *http.Request) error {
	credential, err := h.authenticate(r, true)
	if err != nil {
		return err
	}
	response, err := httpapi.DecodeObjectBodyAs[TunnelResponse](w, r, h.cfg.MaxBodyBytes)
	if err != nil {
		return invalidRequest(err)
	}
	if err := validateTunnelResponse(response, h.cfg); err != nil {
		return invalidRequest(err)
	}
	shardToken := r.Header.Get(shardTokenHeader)
	if shardToken == "" {
		return invalidRequest(errors.New("X-Tunnel-Shard-Token is required"))
	}
	if len(shardToken) > maxShardTokenBytes {
		return invalidRequest(errors.New("X-Tunnel-Shard-Token exceeds the configured limit"))
	}
	instanceID, err := connectorInstanceID(r, false)
	if err != nil {
		return invalidRequest(err)
	}
	err = h.broker.SubmitResponse(r.Context(), credential.TunnelUUID, instanceID, credential.TokenVersion, shardToken, *response)
	if err != nil {
		if errors.Is(err, ErrRequestNotFound) || errors.Is(err, ErrResponseMismatch) || errors.Is(err, ErrRequestExpired) {
			return connectorRequestNotFound()
		}
		return unavailable("Tunnel broker is unavailable", err)
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (h *ConnectorHandler) authenticate(r *http.Request, allowRetired bool) (connectorAuthContext, error) {
	token := auth.ExtractBearerToken(r)
	if token == "" {
		return connectorAuthContext{}, invalidConnectorCredential()
	}
	hash := sha256.Sum256([]byte(token))
	context, err := h.db.FindMCPTunnelTokenContext(r.Context(), chi.URLParam(r, "tunnel_id"), hash[:])
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return connectorAuthContext{}, invalidConnectorCredential()
		}
		return connectorAuthContext{}, internalError("Could not authenticate tunnel connector", fmt.Errorf("lookup tunnel token: %w", err))
	}
	if context.TunnelArchivedAt != nil || context.Token.ArchivedAt != nil || (!allowRetired && context.Token.RetiredAt != nil) {
		return connectorAuthContext{}, invalidConnectorCredential()
	}
	return connectorAuthContext{
		TunnelUUID: context.Token.TunnelUUID, TunnelExternalID: context.TunnelExternalID,
		OrganizationUUID: context.OrganizationUUID, WorkspaceUUID: context.WorkspaceUUID,
		TokenVersion: context.Token.Version,
	}, nil
}

func (h *ConnectorHandler) pollOptions(r *http.Request) (int, time.Duration, error) {
	limit := defaultPollLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxPollLimit {
			return 0, 0, errors.New("limit must be between 1 and 25")
		}
		limit = parsed
	}
	timeout := h.cfg.PollTimeout
	if raw := r.URL.Query().Get("timeout_ms"); raw != "" {
		milliseconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || milliseconds < 0 {
			return 0, 0, errors.New("timeout_ms must be a non-negative integer")
		}
		maxMilliseconds := timeout.Milliseconds()
		if milliseconds <= maxMilliseconds {
			timeout = time.Duration(milliseconds) * time.Millisecond
		}
	}
	return limit, timeout, nil
}

func filterPollChannels(declarations []ChannelDeclaration, requested []string) ([]ChannelDeclaration, error) {
	if len(requested) == 0 {
		return declarations, nil
	}
	available := make(map[string]ChannelDeclaration, len(declarations))
	for _, declaration := range declarations {
		available[declaration.Name] = declaration
	}
	filtered := make([]ChannelDeclaration, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		if !channelNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid tunnel channel %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate tunnel channel %q", name)
		}
		declaration, exists := available[name]
		if !exists {
			return nil, fmt.Errorf("tunnel channel %q is not declared by the connector", name)
		}
		seen[name] = struct{}{}
		filtered = append(filtered, declaration)
	}
	return filtered, nil
}

func validateTunnelResponse(response *TunnelResponse, cfg config.TunnelConfig) error {
	if !requestIDPattern.MatchString(response.RequestID) {
		return errors.New("request_id is invalid")
	}
	if response.ResponseCode != 0 && (response.ResponseCode < 200 || response.ResponseCode > 599) {
		return errors.New("resp_code must be a valid final HTTP status code")
	}
	if response.Channel == "" {
		response.Channel = "main"
	}
	if !channelNamePattern.MatchString(response.Channel) {
		return errors.New("channel is invalid")
	}
	if response.ResponseType == "" {
		response.ResponseType = ResponseTypeJSONRPC
	}
	switch response.ResponseType {
	case ResponseTypeJSONRPCNotify, ResponseTypeJSONRPC, ResponseTypeOAuth:
		if len(response.JSONResponse) == 0 {
			return errors.New("resp_json is required for this response type")
		}
	case ResponseTypeNotifyAck, ResponseTypeSessionTermination:
		if len(response.JSONResponse) != 0 {
			return errors.New("resp_json must be omitted for acknowledgment responses")
		}
	default:
		return errors.New("resp_type is invalid")
	}
	return validateConnectorResponseHeaders(response.ResponseHeaders, response.ResponseType, cfg)
}

func connectorInstanceID(r *http.Request, required bool) (string, error) {
	instanceID := r.Header.Get(connectorInstanceHeader)
	if instanceID == "" {
		if required {
			return "", errors.New("X-Tunnel-Client-Instance-Id is required for process-affine channels")
		}
		return "legacy", nil
	}
	if strings.TrimSpace(instanceID) != instanceID {
		return "", errors.New("X-Tunnel-Client-Instance-Id must not contain surrounding whitespace")
	}
	if len(instanceID) > maxConnectorInstanceID {
		return "", errors.New("X-Tunnel-Client-Instance-Id exceeds the configured limit")
	}
	return instanceID, nil
}

func channelsRequireProcessAffinity(channels []ChannelDeclaration) bool {
	for _, channel := range channels {
		if channel.ProcessAffinity {
			return true
		}
	}
	return false
}

func validateConnectorResponseHeaders(headers http.Header, responseType ResponseType, cfg config.TunnelConfig) error {
	// Content-Length and Date describe the connector's private HTTP response.
	// OMA may rewrite the body and always emits its own transport metadata, so
	// carrying either value across the tunnel would be stale or duplicated.
	headers.Del("Content-Length")
	headers.Del("Date")
	var total int64
	for name, values := range headers {
		if !connectorResponseHeaderAllowed(name, responseType) {
			return fmt.Errorf("response header %q is not allowed", name)
		}
		total += int64(len(name))
		for _, value := range values {
			if int64(len(value)) > cfg.MaxHeaderValueBytes {
				return errors.New("response header value exceeds the configured limit")
			}
			total += int64(len(value))
		}
	}
	if total > cfg.MaxHeaderBytes {
		return errors.New("response headers exceed the configured limit")
	}
	return nil
}

func connectorResponseHeaderAllowed(name string, responseType ResponseType) bool {
	switch strings.ToLower(name) {
	case "access-control-expose-headers", "content-type", "last-event-id", "mcp-protocol-version", "mcp-session-id", "www-authenticate":
		return true
	case "cache-control", "etag", "expires", "last-modified", "location", "pragma", "retry-after", "vary":
		return responseType == ResponseTypeOAuth
	default:
		return false
	}
}
