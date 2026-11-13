package main

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
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

const sessionPortForwardShutdownTimeout = 10 * time.Second

type sessionPortRuntime struct {
	settings *sessionports.Settings
	registry *porttunnel.Registry
	router   porttunnel.Router
}

func openSessionPorts(ctx context.Context, cfg config.Config, redisClient *redis.Client, natsConnection *nats.Conn, logger *slog.Logger) (sessionPortRuntime, error) {
	settings, err := sessionports.New(cfg, logger.With("component", "session_port_gateway"))
	if err != nil || !settings.Enabled() {
		return sessionPortRuntime{settings: settings}, err
	}
	owner := cfg.SessionPorts.InternalAdvertiseAddr + "#" + uuid.NewString()
	owners := porttunnel.NewRedisOwnerStore(redisClient)
	registry := porttunnel.NewRegistry(owner, owners, porttunnel.NewNATSOnlineNotifier(natsConnection), logger.With("component", "session_port_tunnel"))
	go registry.Run(ctx)
	runtime := sessionPortRuntime{settings: settings, registry: registry}
	if !cfg.SessionPorts.ForwardingEnabled() {
		return runtime, nil
	}
	logger = logger.With("component", "session_port_forwarding")
	signer := porttunnel.NewForwardSigner(settings.ForwardKey())
	listener, err := net.Listen("tcp", cfg.SessionPorts.InternalListenAddr)
	if err != nil {
		return sessionPortRuntime{}, fmt.Errorf("listen session port forwarding: %w", err)
	}
	server := porttunnel.NewForwardServer(registry, signer, cfg.SessionPorts.MaxRequestBodyBytes, cfg.SessionPorts.ResponseIdleTimeout, logger).HTTPServer()
	go func() {
		logger.Info("session port forwarding listening", "addr", cfg.SessionPorts.InternalListenAddr, "advertise_addr", cfg.SessionPorts.InternalAdvertiseAddr)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("session port forwarding server stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		registry.CloseAll()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), sessionPortForwardShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	}()
	runtime.router = porttunnel.NewClusterRouter(registry, owners, porttunnel.NewForwardClient(signer), logger)
	return runtime, nil
}
