package porttunnel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const forwardShutdownTimeout = 10 * time.Second

type Options struct {
	AdvertiseAddr       string
	ListenAddr          string
	ForwardKey          []byte
	MaxRequestBodyBytes int64
	ResponseIdleTimeout time.Duration
}

type Deps struct {
	Redis         *redis.Client
	NATS          *nats.Conn
	Logger        *slog.Logger
	ForwardLogger *slog.Logger
}

type Runtime struct {
	Registry *Registry
	Router   Router
	forward  *http.Server
}

func Start(ctx context.Context, deps Deps, options Options) (Runtime, error) {
	owner := options.AdvertiseAddr + "#" + uuid.NewString()
	notifier := NewNATSOnlineNotifier(deps.NATS)
	if options.ListenAddr == "" {
		registry := NewRegistry(owner, nil, notifier, deps.Logger)
		return Runtime{Registry: registry, Router: registry}, nil
	}
	logger := logging.LoggerOrDefault(deps.ForwardLogger)
	listener, err := net.Listen("tcp", options.ListenAddr)
	if err != nil {
		return Runtime{}, fmt.Errorf("listen session port forwarding: %w", err)
	}
	owners := NewRedisOwnerStore(deps.Redis)
	registry := NewRegistry(owner, owners, notifier, deps.Logger)
	go registry.Run(ctx)
	signer := NewForwardSigner(options.ForwardKey)
	server := NewForwardServer(registry, signer, options.MaxRequestBodyBytes, options.ResponseIdleTimeout, logger).HTTPServer()
	go serveForwarding(server, listener, options, logger)
	return Runtime{
		Registry: registry,
		Router:   NewClusterRouter(registry, owners, NewForwardClient(signer), logger),
		forward:  server,
	}, nil
}

func (r Runtime) Close() {
	if r.forward != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), forwardShutdownTimeout)
		defer cancel()
		if err := r.forward.Shutdown(shutdownCtx); err != nil {
			_ = r.forward.Close()
		}
	}
	if r.Registry != nil {
		r.Registry.CloseAll()
	}
}

func serveForwarding(server *http.Server, listener net.Listener, options Options, logger *slog.Logger) {
	logger.Info("session port forwarding listening", "addr", options.ListenAddr, "advertise_addr", options.AdvertiseAddr)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("session port forwarding server stopped", "error", err)
	}
}
