package natsclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const clientName = "open-managed-agents"

// Open establishes the process-wide NATS connection and verifies that the
// configured server provides JetStream before returning it to the assembly layer.
func Open(ctx context.Context, cfg config.NATSConfig, logger *slog.Logger) (*nats.Conn, error) {
	serverURL := strings.TrimSpace(cfg.URL)
	if serverURL == "" {
		return nil, errors.New("nats.url is required")
	}
	logger = logging.LoggerOrDefault(logger)

	options := connectionOptions(cfg, logger)
	connection, err := nats.Connect(serverURL, options...)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}

	jetStream, err := jetstream.New(connection)
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("create nats JetStream client: %w", err)
	}
	if _, err := jetStream.AccountInfo(ctx); err != nil {
		connection.Close()
		return nil, fmt.Errorf("verify nats JetStream: %w", err)
	}
	return connection, nil
}

func connectionOptions(cfg config.NATSConfig, logger *slog.Logger) []nats.Option {
	return []nats.Option{
		nats.Name(clientName),
		nats.Timeout(cfg.ConnectTimeout),
		nats.DrainTimeout(cfg.DrainTimeout),
		nats.DisconnectErrHandler(func(_ *nats.Conn, disconnectErr error) {
			if disconnectErr != nil {
				logger.Warn("nats disconnected", "error", disconnectErr)
			}
		}),
		nats.ReconnectHandler(func(*nats.Conn) {
			logger.Info("nats reconnected")
		}),
		nats.ClosedHandler(func(closedConnection *nats.Conn) {
			if closeErr := closedConnection.LastError(); closeErr != nil {
				logger.Warn("nats connection closed", "error", closeErr)
			}
		}),
		nats.ErrorHandler(func(_ *nats.Conn, subscription *nats.Subscription, asyncErr error) {
			attrs := []any{"error", asyncErr}
			if subscription != nil {
				attrs = append(attrs, "subject", subscription.Subject)
			}
			logger.Error("nats asynchronous error", attrs...)
		}),
	}
}
