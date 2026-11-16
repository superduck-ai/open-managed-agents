package codesessions

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const toolCallWakeFlushTimeout = 2 * time.Second

func (h *Handler) WithToolCallNotifications(connection *nats.Conn) *Handler {
	h.toolCallNotifications = connection
	return h
}

func (h *Handler) toolCallWake(ctx context.Context, subjects []string, wait time.Duration) (func() bool, func()) {
	wake := make(chan struct{}, 1)
	var subscriptions []*nats.Subscription
	if h.toolCallNotifications != nil {
		for _, subject := range subjects {
			subscription, _ := h.toolCallNotifications.Subscribe(subject, func(*nats.Msg) {
				select {
				case wake <- struct{}{}:
				default:
				}
			})
			if subscription != nil {
				subscriptions = append(subscriptions, subscription)
			}
		}
		if len(subscriptions) > 0 {
			flushCtx, cancel := context.WithTimeout(ctx, toolCallWakeFlushTimeout)
			_ = h.toolCallNotifications.FlushWithContext(flushCtx)
			cancel()
		}
	}
	timer := time.NewTimer(wait)
	next := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-wake:
			return true
		}
	}
	closeWait := func() {
		timer.Stop()
		for _, subscription := range subscriptions {
			_ = subscription.Unsubscribe()
		}
	}
	return next, closeWait
}

func (h *Handler) notifyToolCall(call db.SandboxToolCall) {
	if h.toolCallNotifications == nil {
		return
	}
	subject := "oma.tc." + call.UUID + ".done"
	switch {
	case call.State == "pending":
		subject = "oma.cs." + call.CodeSessionUUID + ".calls"
	case !toolCallDone(call.State):
		return
	}
	if err := h.toolCallNotifications.Publish(subject, nil); err != nil {
		h.logger.Warn("tool call notification failed", "call_uuid", call.UUID, "error", err)
	}
}

func (h *Handler) RunToolCallMaintenance(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			calls, err := h.db.ClaimDueSandboxToolCallFinalizations(ctx)
			if err != nil {
				h.logger.ErrorContext(ctx, "tool call finalization scan failed", "error", err)
			}
			for _, call := range calls {
				h.finalizeToolCall(ctx, call, false)
			}
			changes, err := h.db.SweepSandboxToolCalls(ctx)
			for _, call := range changes {
				h.notifyToolCall(call)
			}
			if err != nil {
				h.logger.ErrorContext(ctx, "tool call maintenance failed", "error", err)
			}
		}
	}
}
