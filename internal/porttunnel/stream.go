package porttunnel

import (
	"context"
	"errors"
	"sync"
	"time"
)

type streamEvent struct {
	headers *httpHeadersMsg
	data    []byte
	final   bool
	err     error
}

type wsEvent struct {
	opened   bool
	protocol string
	data     []byte
	binary   bool
	closed   *CloseError
	err      error
}

type queuedEvent[T any] struct {
	event   T
	size    int
	payload int
}

type queue[T any] struct {
	id          string
	mu          sync.Mutex
	items       []queuedEvent[T]
	bytes       int
	ready       chan struct{}
	progress    chan struct{}
	done        chan struct{}
	doneOnce    sync.Once
	abort       chan struct{}
	abortOnce   sync.Once
	abortErr    error
	releaseOnce sync.Once
	received    int64
	consumed    int64
	acked       int64
	sendCredit  int64
	credit      chan struct{}
}

type (
	httpStream = queue[streamEvent]
	wsStream   = queue[wsEvent]
)

func newQueue[T any]() *queue[T] {
	return &queue[T]{
		id:         newStreamID(),
		ready:      make(chan struct{}, 1),
		progress:   make(chan struct{}, 1),
		credit:     make(chan struct{}, 1),
		sendCredit: FlowControlWindow,
		done:       make(chan struct{}),
		abort:      make(chan struct{}),
	}
}

func signal(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

func (q *queue[T]) isDone() bool {
	select {
	case <-q.done:
		return true
	default:
		return false
	}
}

func (q *queue[T]) markDone() {
	q.doneOnce.Do(func() { close(q.done) })
}

func (q *queue[T]) fail(err error) {
	q.abortOnce.Do(func() {
		q.abortErr = err
		close(q.abort)
	})
}

func (q *queue[T]) tryPush(event T, size, limit int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.bytes > 0 && q.bytes+size > limit {
		return false
	}
	q.items = append(q.items, queuedEvent[T]{event: event, size: size})
	q.bytes += size
	signal(q.ready)
	return true
}

func (q *queue[T]) pushWithinWindow(event T, cost int, merge func(*T, T) bool) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if cost > 0 && q.received >= q.acked+FlowControlWindow {
		return false
	}
	q.received += int64(cost)
	if last := len(q.items) - 1; merge != nil && last >= 0 && merge(&q.items[last].event, event) {
		q.items[last].payload += cost
		q.items[last].size += cost
		q.bytes += cost
		signal(q.ready)
		return true
	}
	q.items = append(q.items, queuedEvent[T]{event: event, size: cost + eventOverheadBytes, payload: cost})
	q.bytes += cost + eventOverheadBytes
	signal(q.ready)
	return true
}

func mergeHTTPChunk(tail *streamEvent, next streamEvent) bool {
	if !plainChunk(*tail) || !plainChunk(next) || len(tail.data)+len(next.data) > mergedChunkLimit {
		return false
	}
	tail.data = append(tail.data, next.data...)
	return true
}

func plainChunk(event streamEvent) bool {
	return event.headers == nil && !event.final && event.err == nil && len(event.data) > 0
}

func (q *queue[T]) pop(flowControl bool) (T, int64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var zero T
	if len(q.items) == 0 {
		return zero, 0, false
	}
	head := q.items[0]
	q.items[0] = queuedEvent[T]{}
	q.items = q.items[1:]
	q.bytes -= head.size
	if len(q.items) > 0 {
		signal(q.ready)
	}
	signal(q.progress)
	if !flowControl {
		return head.event, 0, true
	}
	q.consumed += int64(head.payload)
	grant := q.consumed - q.acked
	if grant < FlowControlWindow/2 {
		return head.event, 0, true
	}
	q.acked = q.consumed
	return head.event, grant, true
}

func (q *queue[T]) take(conn *Conn) (T, bool) {
	event, grant, ok := q.pop(conn.flowControl)
	if grant > 0 {
		conn.sendWindowUpdate(q.id, grant)
	}
	return event, ok
}

func (q *queue[T]) grantCredit(bytes int64) {
	q.mu.Lock()
	q.sendCredit += bytes
	q.mu.Unlock()
	signal(q.credit)
}

func (q *queue[T]) spendCredit(ctx context.Context, conn *Conn, bytes int) error {
	for {
		q.mu.Lock()
		if q.sendCredit > 0 {
			q.sendCredit -= int64(bytes)
			q.mu.Unlock()
			return nil
		}
		q.mu.Unlock()
		select {
		case <-q.credit:
		case <-q.done:
			return ErrTunnelDisconnected
		case <-q.abort:
			return q.abortErr
		case <-conn.closed:
			return ErrTunnelDisconnected
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (q *queue[T]) next(ctx context.Context, conn *Conn, idle time.Duration) (T, error) {
	var zero T
	var timeout <-chan time.Time
	if idle > 0 {
		timer := time.NewTimer(idle)
		defer timer.Stop()
		timeout = timer.C
	}
	for {
		if event, ok := q.take(conn); ok {
			return event, nil
		}
		select {
		case <-q.ready:
		case <-q.abort:
			return zero, q.abortErr
		case <-conn.closed:
			if event, ok := q.take(conn); ok {
				return event, nil
			}
			return zero, ErrTunnelDisconnected
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-timeout:
			return zero, context.DeadlineExceeded
		}
	}
}

func (c *Conn) registerHTTP(stream *httpStream) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpStreams[stream.id] = stream
}

func (c *Conn) registerWS(stream *wsStream) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wsStreams[stream.id] = stream
}

func (c *Conn) finishHTTP(stream *httpStream) {
	stream.markDone()
	stream.releaseOnce.Do(func() {
		c.mu.Lock()
		delete(c.httpStreams, stream.id)
		c.mu.Unlock()
		<-c.requests
	})
}

func (c *Conn) finishWS(stream *wsStream) {
	stream.markDone()
	stream.releaseOnce.Do(func() {
		c.mu.Lock()
		delete(c.wsStreams, stream.id)
		c.mu.Unlock()
		<-c.websockets
	})
}

func (c *Conn) abortHTTP(stream *httpStream, err error) {
	c.mu.Lock()
	delete(c.httpStreams, stream.id)
	c.mu.Unlock()
	stream.fail(err)
	c.enqueueControl(tunnelRequest{HTTPCancel: &httpCancelMsg{RequestID: stream.id}})
}

func (c *Conn) abortWS(stream *wsStream, err error) {
	c.mu.Lock()
	delete(c.wsStreams, stream.id)
	c.mu.Unlock()
	stream.fail(err)
	reason := "caller stopped reading"
	if errors.Is(err, ErrFlowControlViolation) {
		reason = "flow control window exceeded"
	}
	c.enqueueControl(tunnelRequest{WSClose: &wsCloseMsg{TunnelID: stream.id, Code: 1008, Reason: reason}})
}
