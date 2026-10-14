package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type consumeSession struct {
	context jetstream.ConsumeContext
	closed  <-chan struct{}
}

type workerRuntime struct {
	worker    *Worker
	ctx       context.Context
	cancel    context.CancelFunc
	timeout   time.Duration
	client    *http.Client
	transport *http.Transport
	mutex     sync.Mutex
	sessions  []*consumeSession
	stopping  bool
	pending   sync.WaitGroup
	once      sync.Once
	done      chan struct{}
}

func newWorkerRuntime(ctx context.Context, worker *Worker) *workerRuntime {
	ctx, cancel := context.WithCancel(ctx)
	timeout := webhookTimeout(worker.cfg)
	concurrency := webhookConcurrency(worker.cfg)
	transport := newDeliveryTransport(ctx, worker.cfg.AllowInsecure, timeout, concurrency)
	return &workerRuntime{
		worker: worker, ctx: ctx, cancel: cancel, timeout: timeout, transport: transport,
		client:   &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		sessions: make([]*consumeSession, concurrency),
		done:     make(chan struct{}),
	}
}

func (r *workerRuntime) consume(index int) (*consumeSession, error) {
	session, err := r.worker.queue.consumer.Consume(func(msg jetstream.Msg) {
		ctx, cancel := context.WithTimeout(r.ctx, r.timeout+15*time.Second)
		defer cancel()
		defer func() {
			if recover() != nil {
				r.worker.logger.ErrorContext(ctx, "webhook message processing panicked", "consume_slot", index, "stack", string(debug.Stack()))
			}
		}()
		r.worker.processMessage(ctx, r.client, msg)
	}, jetstream.PullMaxMessages(1), jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		if r.ctx.Err() == nil {
			r.worker.logger.ErrorContext(r.ctx, "webhook consumption failed", "consume_slot", index, "error", err)
		}
	}))
	if err != nil {
		return nil, fmt.Errorf("start webhook consume slot %d: %w", index, err)
	}
	return &consumeSession{context: session, closed: session.Closed()}, nil
}

func (r *workerRuntime) runSession(index int, session *consumeSession) {
	for {
		select {
		case <-r.ctx.Done():
			session.context.Stop()
			<-session.closed
			return
		case <-session.closed:
		}
		if r.ctx.Err() != nil {
			return
		}
		if r.worker.queue.js.Conn().IsClosed() {
			r.worker.logger.ErrorContext(r.ctx, "webhook connection closed", "consume_slot", index)
			return
		}
		var ok bool
		session, ok = r.restartSession(index)
		if !ok {
			return
		}
	}
}

func (r *workerRuntime) restartSession(index int) (*consumeSession, bool) {
	for {
		timer := time.NewTimer(time.Second)
		select {
		case <-r.ctx.Done():
			timer.Stop()
			return nil, false
		case <-timer.C:
		}
		r.mutex.Lock()
		if r.stopping || r.ctx.Err() != nil || r.worker.queue.js.Conn().IsClosed() {
			r.mutex.Unlock()
			return nil, false
		}
		session, err := r.consume(index)
		if err == nil {
			r.sessions[index] = session
		}
		r.mutex.Unlock()
		if err == nil {
			return session, true
		}
		r.worker.logger.ErrorContext(r.ctx, "webhook consumption restart failed", "consume_slot", index, "error", err)
	}
}

func (r *workerRuntime) stop() {
	r.once.Do(func() {
		r.mutex.Lock()
		r.stopping = true
		for _, session := range r.sessions {
			if session != nil {
				session.context.Stop()
			}
		}
		r.cancel()
		r.mutex.Unlock()
		r.pending.Wait()
		for _, session := range r.sessions {
			if session != nil {
				<-session.closed
			}
		}
		r.transport.CloseIdleConnections()
		close(r.done)
	})
	<-r.done
}
