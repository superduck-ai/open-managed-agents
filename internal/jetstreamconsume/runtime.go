package jetstreamconsume

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type Options struct {
	Name           string
	Consumer       jetstream.Consumer
	ConnClosed     func() bool
	Concurrency    int
	MessageTimeout time.Duration
	Handle         func(context.Context, jetstream.Msg)
	Logger         *slog.Logger
	OnStop         func()
}

type consumeSession struct {
	context jetstream.ConsumeContext
	closed  <-chan struct{}
}

type Runtime struct {
	options  Options
	ctx      context.Context
	cancel   context.CancelFunc
	mutex    sync.Mutex
	sessions []*consumeSession
	stopping bool
	pending  sync.WaitGroup
	once     sync.Once
	done     chan struct{}
}

func New(ctx context.Context, options Options) *Runtime {
	ctx, cancel := context.WithCancel(ctx)
	options.Logger = logging.LoggerOrDefault(options.Logger)
	return &Runtime{options: options, ctx: ctx, cancel: cancel, sessions: make([]*consumeSession, max(options.Concurrency, 1)), done: make(chan struct{})}
}

func (r *Runtime) Slots() int {
	return len(r.sessions)
}

func (r *Runtime) Start() error {
	for index := range r.sessions {
		session, err := r.consume(index)
		if err != nil {
			r.Stop()
			return err
		}
		r.sessions[index] = session
	}
	for index, session := range r.sessions {
		r.pending.Go(func() { r.runSession(index, session) })
	}
	go func() {
		select {
		case <-r.ctx.Done():
			r.Stop()
		case <-r.done:
		}
	}()
	return nil
}

func (r *Runtime) consume(index int) (*consumeSession, error) {
	session, err := r.options.Consumer.Consume(func(msg jetstream.Msg) {
		ctx, cancel := context.WithTimeout(r.ctx, r.options.MessageTimeout)
		defer cancel()
		defer func() {
			if recover() != nil {
				r.options.Logger.ErrorContext(ctx, "queued message processing panicked", "queue", r.options.Name, "consume_slot", index, "stack", string(debug.Stack()))
			}
		}()
		r.options.Handle(ctx, msg)
	}, jetstream.PullMaxMessages(1), jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		if r.ctx.Err() == nil {
			r.options.Logger.ErrorContext(r.ctx, "queue consumption failed", "queue", r.options.Name, "consume_slot", index, "error", err)
		}
	}))
	if err != nil {
		return nil, fmt.Errorf("start %s consume slot %d: %w", r.options.Name, index, err)
	}
	return &consumeSession{context: session, closed: session.Closed()}, nil
}

func (r *Runtime) runSession(index int, session *consumeSession) {
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
		if r.options.ConnClosed() {
			r.options.Logger.ErrorContext(r.ctx, "queue connection closed", "queue", r.options.Name, "consume_slot", index)
			return
		}
		var ok bool
		session, ok = r.restartSession(index)
		if !ok {
			return
		}
	}
}

func (r *Runtime) restartSession(index int) (*consumeSession, bool) {
	for {
		timer := time.NewTimer(time.Second)
		select {
		case <-r.ctx.Done():
			timer.Stop()
			return nil, false
		case <-timer.C:
		}
		r.mutex.Lock()
		if r.stopping || r.ctx.Err() != nil || r.options.ConnClosed() {
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
		r.options.Logger.ErrorContext(r.ctx, "queue consumption restart failed", "queue", r.options.Name, "consume_slot", index, "error", err)
	}
}

func (r *Runtime) Stop() {
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
		if r.options.OnStop != nil {
			r.options.OnStop()
		}
		close(r.done)
	})
	<-r.done
}
