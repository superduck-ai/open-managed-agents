package porttunnel

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const ForwardMaxHeaderBytes = 4 << 20

type ForwardServer struct {
	registry *Registry
	signer   *ForwardSigner
	maxBody  int64
	idle     time.Duration
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

type forwardTarget struct {
	request forwardRequest
	port    int
	tunnel  Tunnel
}

func NewForwardServer(registry *Registry, signer *ForwardSigner, maxBody int64, idle time.Duration, logger *slog.Logger) *ForwardServer {
	return &ForwardServer{
		registry: registry,
		signer:   signer,
		maxBody:  maxBody,
		idle:     idle,
		logger:   logging.LoggerOrDefault(logger),
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			CheckOrigin:      func(*http.Request) bool { return true },
		},
	}
}

func (s *ForwardServer) HTTPServer() *http.Server {
	return &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    ForwardMaxHeaderBytes,
	}
}

func (s *ForwardServer) Handler() http.Handler {
	router := chi.NewRouter()
	router.Post(forwardPathPrefix+"{code_session_uuid}/{port}", s.serveHTTP)
	router.Get(forwardPathPrefix+"{code_session_uuid}/{port}/ws", s.serveWebSocket)
	return router
}

func (s *ForwardServer) prepare(w http.ResponseWriter, r *http.Request) (forwardTarget, bool) {
	if err := s.signer.verify(r, s.registry.Owner()); err != nil {
		s.logger.WarnContext(r.Context(), "reject session port forward", "remote_addr", r.RemoteAddr, "error", err)
		writeForwardFailure(w, err)
		return forwardTarget{}, false
	}
	port, err := strconv.Atoi(chi.URLParam(r, "port"))
	if err != nil || port < 1 || port > 65535 {
		writeBadForward(w)
		return forwardTarget{}, false
	}
	request, err := decodeForwardRequest(r.Header.Get(forwardRequestHeader))
	if err != nil {
		writeBadForward(w)
		return forwardTarget{}, false
	}
	tunnel, err := s.registry.Lookup(r.Context(), chi.URLParam(r, "code_session_uuid"))
	if err != nil {
		writeForwardFailure(w, err)
		return forwardTarget{}, false
	}
	return forwardTarget{request: request, port: port, tunnel: tunnel}, true
}

func (s *ForwardServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	target, ok := s.prepare(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBody))
	if err != nil {
		writeBadForward(w)
		return
	}
	response, err := target.tunnel.RoundTrip(r.Context(), HTTPRequest{
		Port:    target.port,
		Method:  target.request.Method,
		Path:    target.request.Path,
		Headers: target.request.Headers,
		Body:    body,
	})
	if err != nil {
		if r.Context().Err() == nil {
			writeForwardFailure(w, err)
		}
		return
	}
	defer response.Body.Close()
	flowControl := target.tunnel.FlowControl()
	controller := http.NewResponseController(w)
	writeTimeout := forwardWriteTimeout(flowControl)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_ = controller.SetWriteDeadline(time.Now().Add(writeTimeout))
	w.WriteHeader(http.StatusOK)
	meta, err := json.Marshal(forwardMeta{Status: response.StatusCode, Headers: response.Headers, FlowControl: flowControl})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if !writeForwardChunk(w, controller, append(meta, '\n'), writeTimeout) {
		panic(http.ErrAbortHandler)
	}
	for {
		data, err := response.Body.Next(r.Context(), s.idle)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil || !writeForwardChunk(w, controller, data, writeTimeout) {
			panic(http.ErrAbortHandler)
		}
	}
}

func writeForwardChunk(w http.ResponseWriter, controller *http.ResponseController, data []byte, writeTimeout time.Duration) bool {
	if err := controller.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return false
	}
	if _, err := w.Write(data); err != nil {
		return false
	}
	return controller.Flush() == nil
}

func (s *ForwardServer) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	target, ok := s.prepare(w, r)
	if !ok {
		return
	}
	stream, err := target.tunnel.OpenWebSocket(r.Context(), WebSocketOpen{
		Port:      target.port,
		Path:      target.request.Path,
		Headers:   target.request.Headers,
		Protocols: target.request.Protocols,
	})
	if err != nil {
		if r.Context().Err() == nil {
			writeForwardFailure(w, err)
		}
		return
	}
	flowControl := target.tunnel.FlowControl()
	header := http.Header{}
	if protocol := stream.Protocol(); protocol != "" {
		header.Set(forwardProtocolHeader, protocol)
	}
	if flowControl {
		header.Set(forwardFlowHeader, "1")
	}
	conn, err := s.upgrader.Upgrade(w, r, header)
	if err != nil {
		stream.Close(closeGoingAway, "session port forward handshake failed")
		return
	}
	relayWebSockets(stream, newSocketStream(conn, "", forwardWriteTimeout(flowControl)))
}

func writeBadForward(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(forwardFailure{Code: forwardBadRequest})
}
