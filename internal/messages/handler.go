package messages

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

const maxRequestBodyBytes int64 = 32 << 20

var requestHeadersToRemove = map[string]struct{}{
	"Authorization":       {},
	"Connection":          {},
	"Cookie":              {},
	"Host":                {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Proxy-Connection":    {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
	"X-Api-Key":           {},
	"X-Organization-Uuid": {},
	"X-Workspace-Id":      {},
}

var responseHeadersToRemove = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Proxy-Connection":    {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

type Handler struct {
	cfg     config.Config
	client  *http.Client
	gateway *gateway
}

// Handler proxies Messages requests.
type flushingResponseWriter struct {
	writer     io.Writer
	controller *http.ResponseController
}

// NewHandler creates a Messages proxy handler.
func NewHandler(cfg config.Config) *Handler {
	client := &http.Client{Transport: newProxyTransport()}
	return &Handler{
		cfg:     cfg,
		client:  client,
		gateway: newGateway(cfg, client, websearch.NewProvider(cfg.WebSearch, client)),
	}
}

func newProxyTransport() http.RoundTripper {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
		cloned := transport.Clone()
		cloned.MaxIdleConnsPerHost = 32
		return cloned
	}
	return &http.Transport{MaxIdleConnsPerHost: 32}
}

// Create proxies a Messages request.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}
	if strings.TrimSpace(h.cfg.AnthropicUpstream.APIKey) == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, "api_error", "anthropic_upstream.api_key is required for Messages"))
		return
	}
	if r.ContentLength > maxRequestBodyBytes {
		writeRequestTooLarge(w, r)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.gateway != nil && principal.CredentialType == auth.CredentialTypeCodeSessionOAuth {
		body, candidate, err := readGatewayCandidate(w, r)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeRequestTooLarge(w, r)
				return
			}
			log.Printf("read Messages request: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Could not read request body"))
			return
		}
		if candidate {
			response, handled, gatewayErr := h.gateway.handle(r.Context(), body, r.URL.RawQuery, r.Header)
			if gatewayErr != nil {
				log.Printf("run Messages web search gateway: %v", gatewayErr)
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Messages web search gateway is unavailable"))
				return
			}
			if handled {
				if err := writeProxyResponse(w, &http.Response{StatusCode: response.statusCode, Header: response.header, Body: io.NopCloser(bytes.NewReader(response.body))}); err != nil && r.Context().Err() == nil {
					log.Printf("write Messages web search gateway response: %v", err)
				}
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
	}
	target, err := messagesEndpoint(h.cfg.AnthropicUpstream.BaseURL, r.URL.RawQuery)
	if err != nil {
		log.Printf("build messages upstream endpoint: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Messages upstream is unavailable"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	upstreamRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
	if err != nil {
		log.Printf("build messages upstream request: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Messages upstream is unavailable"))
		return
	}
	upstreamRequest.ContentLength = r.ContentLength
	upstreamRequest.Header = sanitizedRequestHeaders(r.Header)
	upstreamRequest.Header.Set("X-Api-Key", strings.TrimSpace(h.cfg.AnthropicUpstream.APIKey))
	upstreamResponse, err := h.client.Do(upstreamRequest)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeRequestTooLarge(w, r)
			return
		}
		log.Printf("proxy messages upstream request: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Messages upstream is unavailable"))
		return
	}
	defer upstreamResponse.Body.Close()
	if err := writeProxyResponse(w, upstreamResponse); err != nil && r.Context().Err() == nil {
		log.Printf("stream Messages upstream response: %v", err)
	}
}

func readGatewayCandidate(w http.ResponseWriter, r *http.Request) ([]byte, bool, error) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	prefix := make([]byte, 0, 16)
	for {
		var one [1]byte
		n, err := body.Read(one[:])
		if n > 0 {
			prefix = append(prefix, one[0])
			switch one[0] {
			case ' ', '\t', '\r', '\n':
				continue
			case '{':
				rest, readErr := io.ReadAll(body)
				if readErr != nil {
					return nil, false, readErr
				}
				requestBody := append(prefix, rest...)
				return requestBody, true, nil
			default:
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), body))
				return nil, false, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				r.Body = io.NopCloser(bytes.NewReader(prefix))
				return nil, false, nil
			}
			return nil, false, err
		}
	}
}

func writeRequestTooLarge(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds maximum size"))
}

func messagesEndpoint(baseURL string, rawQuery string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("messages upstream base URL must be absolute")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/messages"
	parsed.RawPath = ""
	parsed.RawQuery = rawQuery
	return parsed.String(), nil
}

func sanitizedRequestHeaders(source http.Header) http.Header {
	headers := source.Clone()
	removeConnectionHeaders(headers)
	for name := range requestHeadersToRemove {
		headers.Del(name)
	}
	return headers
}

func copyResponseHeaders(destination http.Header, source http.Header) {
	connectionHeaders := source.Clone()
	removeConnectionHeaders(connectionHeaders)
	for name, values := range connectionHeaders {
		if _, remove := responseHeadersToRemove[http.CanonicalHeaderKey(name)]; remove {
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func removeConnectionHeaders(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
}

func prepareResponseHeaders(headers http.Header) {
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
		headers.Set("Content-Type", contentType)
	}
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return
	}
	if headers.Get("Cache-Control") == "" {
		headers.Set("Cache-Control", "no-cache")
	}
	headers.Set("X-Accel-Buffering", "no")
}

func writeProxyResponse(w http.ResponseWriter, response *http.Response) error {
	copyResponseHeaders(w.Header(), response.Header)
	prepareResponseHeaders(w.Header())
	w.WriteHeader(response.StatusCode)
	controller := http.NewResponseController(w)
	if err := flushProxyResponse(controller); err != nil {
		return err
	}
	writer := flushingResponseWriter{writer: w, controller: controller}
	_, err := io.CopyBuffer(writer, response.Body, make([]byte, 32*1024))
	return err
}

func (w flushingResponseWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	if err != nil {
		return written, err
	}
	if err := flushProxyResponse(w.controller); err != nil {
		return written, err
	}
	return written, nil
}

func flushProxyResponse(controller *http.ResponseController) error {
	err := controller.Flush()
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
