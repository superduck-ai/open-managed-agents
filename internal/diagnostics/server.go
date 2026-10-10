package diagnostics

import (
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/listeners"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

func Start(addr, inheritedFD string, logger *slog.Logger) (func() error, error) {
	if addr == "" {
		return func() error { return nil }, nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("diagnostics address must use a literal loopback IP")
	}
	listener, err := listeners.Open(addr, inheritedFD)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.Handle("GET /debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("GET /debug/pprof/goroutine", pprof.Handler("goroutine"))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	logger = logging.LoggerOrDefault(logger)
	go serve(server, listener, logger)
	return func() error {
		serverErr := server.Close()
		listenerErr := listener.Close()
		if errors.Is(listenerErr, net.ErrClosed) {
			listenerErr = nil
		}
		return errors.Join(serverErr, listenerErr)
	}, nil
}

func serve(server *http.Server, listener net.Listener, logger *slog.Logger) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("diagnostics server failed", "error", err)
	}
}
