package portgateway

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

const (
	sandboxRunning  = "running"
	sandboxCreating = "creating"
)

func (g *Gateway) connect(ctx context.Context, resolved target) (porttunnel.Tunnel, *gatewayError) {
	codeSessionUUID := resolved.session.CodeSessionUUID
	tunnel, err := g.router.Lookup(ctx, codeSessionUUID)
	if err == nil {
		return tunnel, nil
	}
	if !errors.Is(err, porttunnel.ErrNoTunnel) {
		return nil, tunnelDisconnected(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, g.cfg.WakeTimeout)
	defer cancel()
	online := make(chan error, 1)
	go func() { online <- g.router.WaitOnline(waitCtx, codeSessionUUID) }()
	if resolved.session.SandboxState == sandboxRunning {
		if err := g.wake(resolved); errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
			return nil, sandboxUnavailable(err)
		}
	}
	if err := <-online; err != nil {
		return nil, waking(err)
	}
	tunnel, err = g.router.Lookup(ctx, codeSessionUUID)
	if err != nil {
		return nil, waking(err)
	}
	return tunnel, nil
}

func retryWhenOwnerGone[T any](ctx context.Context, g *Gateway, resolved target, tunnel porttunnel.Tunnel, call func(porttunnel.Tunnel) (T, error)) (T, porttunnel.Tunnel, error) {
	result, err := call(tunnel)
	if !errors.Is(err, porttunnel.ErrTunnelGone) {
		return result, tunnel, err
	}
	tunnel, failure := g.connect(ctx, resolved)
	if failure != nil {
		var zero T
		return zero, nil, failure
	}
	result, err = call(tunnel)
	return result, tunnel, err
}

func (g *Gateway) wake(resolved target) error {
	providerSandboxID := resolved.session.ProviderSandboxID
	_, err, _ := g.wakes.Do(providerSandboxID, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), g.cfg.WakeTimeout)
		defer cancel()
		err := g.sandboxes.SetTimeout(ctx, providerSandboxID, g.sandboxTimeout)
		if err == nil {
			g.renewals.allow(providerSandboxID, time.Now())
		} else if !errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
			g.logger.WarnContext(ctx, "wake session port sandbox", "session_id", resolved.session.SessionExternalID, "error", err)
		}
		return nil, err
	})
	return err
}
