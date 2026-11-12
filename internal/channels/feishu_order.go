package channels

import "context"

type scopeQueue struct {
	held  bool
	queue []*scopeTicket
}

type scopeTicket struct {
	scopeKey string
	ready    chan struct{}
}

func (w *channelWorker) enqueueScope(scopeKey string) *scopeTicket {
	w.mu.Lock()
	defer w.mu.Unlock()
	queue := w.scopes[scopeKey]
	if queue == nil {
		queue = &scopeQueue{}
		w.scopes[scopeKey] = queue
	}
	ticket := &scopeTicket{scopeKey: scopeKey, ready: make(chan struct{})}
	if !queue.held {
		queue.held = true
		close(ticket.ready)
		return ticket
	}
	queue.queue = append(queue.queue, ticket)
	return ticket
}

func (w *channelWorker) waitScope(ctx context.Context, ticket *scopeTicket) error {
	select {
	case <-ticket.ready:
		return nil
	default:
	}
	select {
	case <-ticket.ready:
		return nil
	case <-ctx.Done():
		if w.dropScope(ticket) {
			return ctx.Err()
		}
		return nil
	}
}

func (w *channelWorker) dropScope(ticket *scopeTicket) bool {
	w.mu.Lock()
	queue := w.scopes[ticket.scopeKey]
	if queue != nil {
		for index, item := range queue.queue {
			if item == ticket {
				queue.queue = append(queue.queue[:index], queue.queue[index+1:]...)
				w.mu.Unlock()
				return true
			}
		}
	}
	w.mu.Unlock()
	w.releaseScope(ticket)
	return false
}

func (w *channelWorker) releaseScope(ticket *scopeTicket) {
	w.mu.Lock()
	queue := w.scopes[ticket.scopeKey]
	if queue == nil {
		w.mu.Unlock()
		return
	}
	if len(queue.queue) > 0 {
		next := queue.queue[0]
		queue.queue = queue.queue[1:]
		w.mu.Unlock()
		close(next.ready)
		return
	}
	delete(w.scopes, ticket.scopeKey)
	w.mu.Unlock()
}
