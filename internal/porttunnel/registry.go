package porttunnel

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const ownerOperationTimeout = 2 * time.Second

type Tunnel interface {
	RoundTrip(ctx context.Context, request HTTPRequest) (*HTTPResponse, error)
	OpenWebSocket(ctx context.Context, open WebSocketOpen) (WebSocketStream, error)
	FlowControl() bool
}

type Router interface {
	Lookup(ctx context.Context, codeSessionUUID string) (Tunnel, error)
	WaitOnline(ctx context.Context, codeSessionUUID string) error
}

type Registry struct {
	owner    string
	owners   OwnerStore
	notifier OnlineNotifier
	logger   *slog.Logger
	mu       sync.Mutex
	conns    map[string]*Conn
	waiters  map[string][]chan struct{}
}

func NewRegistry(owner string, owners OwnerStore, notifier OnlineNotifier, logger *slog.Logger) *Registry {
	return &Registry{
		owner:    owner,
		owners:   owners,
		notifier: notifier,
		logger:   logging.LoggerOrDefault(logger),
		conns:    make(map[string]*Conn),
		waiters:  make(map[string][]chan struct{}),
	}
}

func (r *Registry) register(conn *Conn) {
	key := conn.identity.CodeSessionUUID
	r.mu.Lock()
	previous := r.conns[key]
	r.conns[key] = conn
	waiters := r.waiters[key]
	delete(r.waiters, key)
	r.mu.Unlock()
	if previous != nil {
		previous.Close(errReplaced)
	}
	for _, waiter := range waiters {
		close(waiter)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ownerOperationTimeout)
	defer cancel()
	if r.owners != nil {
		if err := r.owners.Claim(ctx, key, r.owner); err != nil {
			r.logger.WarnContext(ctx, "claim session port tunnel owner", "code_session_id", conn.identity.CodeSessionExternalID, "error", err)
		}
	}
	if r.notifier != nil {
		if err := r.notifier.Publish(key); err != nil {
			r.logger.WarnContext(ctx, "publish session port tunnel online", "code_session_id", conn.identity.CodeSessionExternalID, "error", err)
		}
	}
}

func (r *Registry) unregister(conn *Conn) {
	key := conn.identity.CodeSessionUUID
	r.mu.Lock()
	current := r.conns[key] == conn
	if current {
		delete(r.conns, key)
	}
	r.mu.Unlock()
	if !current || r.owners == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ownerOperationTimeout)
	defer cancel()
	if err := r.owners.Release(ctx, key, r.owner); err != nil {
		r.logger.WarnContext(ctx, "release session port tunnel owner", "code_session_id", conn.identity.CodeSessionExternalID, "error", err)
	}
}

func (r *Registry) Run(ctx context.Context) {
	if r.owners == nil {
		return
	}
	ticker := time.NewTicker(ownerRefreshPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshOwners(ctx)
		}
	}
}

func (r *Registry) refreshOwners(ctx context.Context) {
	r.mu.Lock()
	keys := make([]string, 0, len(r.conns))
	for key := range r.conns {
		keys = append(keys, key)
	}
	r.mu.Unlock()
	for _, key := range keys {
		refreshCtx, cancel := context.WithTimeout(ctx, ownerOperationTimeout)
		if err := r.owners.Refresh(refreshCtx, key, r.owner); err != nil {
			r.logger.WarnContext(refreshCtx, "refresh session port tunnel owner", "code_session_uuid", key, "error", err)
		}
		cancel()
	}
}

func (r *Registry) Lookup(_ context.Context, codeSessionUUID string) (Tunnel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[codeSessionUUID]
	if conn == nil || conn.Err() != nil {
		return nil, ErrNoTunnel
	}
	return conn, nil
}

func (r *Registry) Owner() string {
	return r.owner
}

func (r *Registry) CloseAll() {
	r.mu.Lock()
	conns := make([]*Conn, 0, len(r.conns))
	for _, conn := range r.conns {
		conns = append(conns, conn)
	}
	r.mu.Unlock()
	for _, conn := range conns {
		conn.Close(errShutdown)
		r.unregister(conn)
	}
}

func (r *Registry) WaitOnline(ctx context.Context, codeSessionUUID string) error {
	return r.waitOnline(ctx, codeSessionUUID, nil)
}

func (r *Registry) waitOnline(ctx context.Context, codeSessionUUID string, onlineElsewhere func(context.Context) bool) error {
	local := make(chan struct{})
	r.mu.Lock()
	if conn := r.conns[codeSessionUUID]; conn != nil && conn.Err() == nil {
		r.mu.Unlock()
		return nil
	}
	r.waiters[codeSessionUUID] = append(r.waiters[codeSessionUUID], local)
	r.mu.Unlock()
	defer r.dropWaiter(codeSessionUUID, local)
	signals := []<-chan struct{}{local}
	if r.notifier != nil {
		remote, unsubscribe, err := r.notifier.Subscribe(codeSessionUUID)
		if err != nil {
			r.logger.WarnContext(ctx, "subscribe session port tunnel online", "code_session_uuid", codeSessionUUID, "error", err)
		} else {
			defer unsubscribe()
			signals = append(signals, remote)
		}
	}
	if onlineElsewhere != nil && onlineElsewhere(ctx) {
		return nil
	}
	return waitSignal(ctx, signals...)
}

func (r *Registry) dropWaiter(codeSessionUUID string, waiter chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiters := r.waiters[codeSessionUUID]
	for index, candidate := range waiters {
		if candidate == waiter {
			r.waiters[codeSessionUUID] = append(waiters[:index], waiters[index+1:]...)
			break
		}
	}
	if len(r.waiters[codeSessionUUID]) == 0 {
		delete(r.waiters, codeSessionUUID)
	}
}
