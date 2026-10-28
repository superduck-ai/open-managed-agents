package webhooks

import (
	"context"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/jetstreamconsume"
)

type workerRuntime struct {
	*jetstreamconsume.Runtime
	transport *http.Transport
}

func newWorkerRuntime(ctx context.Context, worker *Worker) *workerRuntime {
	timeout := webhookTimeout(worker.cfg)
	concurrency := webhookConcurrency(worker.cfg)
	transport := newDeliveryTransport(ctx, worker.cfg.AllowInsecure, timeout, concurrency)
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	runtime := jetstreamconsume.New(ctx, jetstreamconsume.Options{
		Name: "webhook", Consumer: worker.queue.consumer, ConnClosed: func() bool { return worker.queue.js.Conn().IsClosed() },
		Concurrency: concurrency, MessageTimeout: timeout + 15*time.Second, Logger: worker.logger,
		Handle: func(ctx context.Context, msg jetstream.Msg) { worker.processMessage(ctx, client, msg) },
		OnStop: transport.CloseIdleConnections,
	})
	return &workerRuntime{Runtime: runtime, transport: transport}
}
