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
}

func Start(ctx context.Context, deps Deps, options Options) (Runtime, error) {
	owners := NewRedisOwnerStore(deps.Redis)
	registry := NewRegistry(options.AdvertiseAddr+"#"+uuid.NewString(), owners, NewNATSOnlineNotifier(deps.NATS), deps.Logger)
	go registry.Run(ctx)
	if options.ListenAddr == "" {
		return Runtime{Registry: registry, Router: registry}, nil
	}
	logger := logging.LoggerOrDefault(deps.ForwardLogger)
	signer := NewForwardSigner(options.ForwardKey)
	listener, err := net.Listen("tcp", options.ListenAddr)
	if err != nil {
		return Runtime{}, fmt.Errorf("listen session port forwarding: %w", err)
	}
	server := NewForwardServer(registry, signer, options.MaxRequestBodyBytes, options.ResponseIdleTimeout, logger).HTTPServer()
	go serveForwarding(server, listener, options, logger)
	go stopForwarding(ctx, registry, server)
	return Runtime{
		Registry: registry,
		Router:   NewClusterRouter(registry, owners, NewForwardClient(signer), logger),
	}, nil
}

func serveForwarding(server *http.Server, listener net.Listener, options Options, logger *slog.Logger) {
	logger.Info("session port forwarding listening", "addr", options.ListenAddr, "advertise_addr", options.AdvertiseAddr)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("session port forwarding server stopped", "error", err)
	}
}

func stopForwarding(ctx context.Context, registry *Registry, server *http.Server) {
	<-ctx.Done()
	registry.CloseAll()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), forwardShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}
}
