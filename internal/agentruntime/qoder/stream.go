package qoder

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	subscriberBufferSize = 256
	maxSSELineBytes      = 4 * 1024 * 1024
)

type streamHubKey struct {
	ProfileUUID     string
	RemoteSessionID string
	ThreadID        string
}

type streamHub struct {
	rt          *Runtime
	idleTimeout time.Duration
	mu          sync.Mutex
	entries     map[streamHubKey]*hubEntry
	closed      bool
}

type hubEntry struct {
	hub        *streamHub
	key        streamHubKey
	scope      agentruntime.Scope
	binding    db.RuntimeResourceBinding
	ctx        context.Context
	cancel     context.CancelFunc
	ready      chan struct{}
	startErr   error
	mu         sync.Mutex
	subs       map[*hubSubscriber]struct{}
	previewMap map[string]string
	idleTimer  *time.Timer
	done       bool
	doneErr    error
}

type hubSubscriber struct {
	entry         *hubEntry
	allowedDeltas map[string]struct{}
	ch            chan agentruntime.StreamFrame
	closed        chan struct{}
	closeOnce     sync.Once
	mu            sync.Mutex
	err           error
}

type directEventStream struct {
	rt      *Runtime
	scope   agentruntime.Scope
	binding db.RuntimeResourceBinding
	cancel  context.CancelFunc
	resp    *http.Response
	reader  *sseFrameReader
	once    sync.Once
}

func newStreamHub(rt *Runtime, idleTimeout time.Duration) *streamHub {
	if idleTimeout <= 0 {
		idleTimeout = defaultHubIdleTimeout
	}
	return &streamHub{
		rt:          rt,
		idleTimeout: idleTimeout,
		entries:     make(map[streamHubKey]*hubEntry),
	}
}

func (h *streamHub) close() {
	h.mu.Lock()
	h.closed = true
	entries := make([]*hubEntry, 0, len(h.entries))
	for k, e := range h.entries {
		entries = append(entries, e)
		delete(h.entries, k)
	}
	h.mu.Unlock()
	for _, e := range entries {
		e.stop(io.EOF)
	}
}

func (r *Runtime) StreamEvents(ctx context.Context, scope agentruntime.Scope, sessionID string, opts agentruntime.StreamOptions) (agentruntime.EventStream, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.LastEventID) != "" {
		return r.openDirectStream(ctx, scope, binding, opts)
	}
	return r.hub.subscribe(ctx, scope, binding, opts)
}

func (r *Runtime) openDirectStream(
	ctx context.Context,
	scope agentruntime.Scope,
	binding db.RuntimeResourceBinding,
	opts agentruntime.StreamOptions,
) (agentruntime.EventStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	resp, err := r.dialSSE(streamCtx, binding.RemoteID, opts.ThreadID, opts.DeltaTypes, DecodeEventID(strings.TrimSpace(opts.LastEventID)))
	if err != nil {
		cancel()
		return nil, err
	}
	return &directEventStream{
		rt:      r,
		scope:   scope,
		binding: binding,
		cancel:  cancel,
		resp:    resp,
		reader:  newSSEFrameReader(resp.Body),
	}, nil
}

func (r *Runtime) dialSSE(
	ctx context.Context,
	remoteSessionID string,
	threadID string,
	deltaTypes []string,
	lastEventID string,
) (*http.Response, error) {
	params := url.Values{}
	if threadID != "" {
		params.Set("session_thread_id", threadID)
	}
	for _, dt := range deltaTypes {
		if dt != "" {
			params.Add("event_deltas[]", dt)
		}
	}
	path := "/sessions/" + url.PathEscape(remoteSessionID) + "/events/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.buildURL(path, params), nil)
	if err != nil {
		return nil, upstreamTransportError(err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := r.streamClient.Do(req)
	if err != nil {
		return nil, upstreamTransportError(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, readAndMapUpstreamError(resp)
	}
	return resp, nil
}

func (s *directEventStream) Next(ctx context.Context) (agentruntime.StreamFrame, error) {
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return agentruntime.StreamFrame{}, err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = s.Close()
	})
	defer stop()

	rawFrame, err := s.reader.readNext()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return agentruntime.StreamFrame{}, ctxErr
		}
		return agentruntime.StreamFrame{}, err
	}
	encoded, err := encodeStreamFrame(rawFrame)
	if err != nil {
		return agentruntime.StreamFrame{}, err
	}
	s.rt.handleStreamSideEffects(ctx, s.scope, s.binding, encoded)
	return encoded, nil
}

func (s *directEventStream) Close() error {
	s.once.Do(func() {
		s.cancel()
		if s.resp != nil && s.resp.Body != nil {
			_ = s.resp.Body.Close()
		}
	})
	return nil
}

func (h *streamHub) subscribe(
	ctx context.Context,
	scope agentruntime.Scope,
	binding db.RuntimeResourceBinding,
	opts agentruntime.StreamOptions,
) (agentruntime.EventStream, error) {
	key := streamHubKey{
		ProfileUUID:     h.rt.profile.UUID,
		RemoteSessionID: binding.RemoteID,
		ThreadID:        opts.ThreadID,
	}
	for {
		entry, sub, created, err := h.getOrCreateEntryWithSub(key, scope, binding, opts.DeltaTypes)
		if err != nil {
			return nil, err
		}
		if created {
			go entry.run()
		}
		select {
		case <-ctx.Done():
			_ = sub.Close()
			return nil, ctx.Err()
		case <-entry.ready:
		}
		if entry.startErr != nil {
			_ = sub.Close()
			return nil, entry.startErr
		}
		return sub, nil
	}
}

func (h *streamHub) getOrCreateEntryWithSub(
	key streamHubKey,
	scope agentruntime.Scope,
	binding db.RuntimeResourceBinding,
	deltaTypes []string,
) (*hubEntry, *hubSubscriber, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, false, agentruntime.UnavailableError("qoder runtime stream hub closed", nil)
	}
	if existing, ok := h.entries[key]; ok {
		if sub, added := existing.addSubscriber(deltaTypes); added {
			return existing, sub, false, nil
		}
	}
	upstreamCtx, cancel := context.WithCancel(context.Background())
	entry := &hubEntry{
		hub:        h,
		key:        key,
		scope:      scope,
		binding:    binding,
		ctx:        upstreamCtx,
		cancel:     cancel,
		ready:      make(chan struct{}),
		subs:       make(map[*hubSubscriber]struct{}),
		previewMap: make(map[string]string),
	}
	sub, _ := entry.addSubscriber(deltaTypes)
	h.entries[key] = entry
	return entry, sub, true, nil
}

func (e *hubEntry) run() {
	resp, err := e.hub.rt.dialSSE(
		e.ctx,
		e.key.RemoteSessionID,
		e.key.ThreadID,
		[]string{"agent.message", "agent.thinking"},
		"",
	)
	if err != nil {
		e.startErr = err
		close(e.ready)
		e.removeFromHub()
		e.stop(err)
		return
	}
	close(e.ready)

	defer func() {
		_ = resp.Body.Close()
		e.cancel()
	}()

	reader := newSSEFrameReader(resp.Body)
	for {
		rawFrame, readErr := reader.readNext()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || e.ctx.Err() != nil {
				e.stop(io.EOF)
			} else {
				e.stop(upstreamTransportError(readErr))
			}
			return
		}
		encoded, encodeErr := encodeStreamFrame(rawFrame)
		if encodeErr != nil {
			continue
		}
		e.hub.rt.handleStreamSideEffects(e.ctx, e.scope, e.binding, encoded)
		e.broadcast(encoded)
	}
}

func (e *hubEntry) addSubscriber(deltaTypes []string) (*hubSubscriber, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done {
		return nil, false
	}
	if e.idleTimer != nil {
		e.idleTimer.Stop()
		e.idleTimer = nil
	}
	allowed := make(map[string]struct{}, len(deltaTypes))
	for _, dt := range deltaTypes {
		if dt != "" {
			allowed[dt] = struct{}{}
		}
	}
	sub := &hubSubscriber{
		entry:         e,
		allowedDeltas: allowed,
		ch:            make(chan agentruntime.StreamFrame, subscriberBufferSize),
		closed:        make(chan struct{}),
	}
	e.subs[sub] = struct{}{}
	return sub, true
}

func (e *hubEntry) removeSubscriber(sub *hubSubscriber) {
	e.mu.Lock()
	delete(e.subs, sub)
	e.scheduleIdleLocked()
	e.mu.Unlock()
}

func (e *hubEntry) scheduleIdleLocked() {
	if !e.done && len(e.subs) == 0 && e.idleTimer == nil {
		e.idleTimer = time.AfterFunc(e.hub.idleTimeout, func() {
			e.onIdleTimeout()
		})
	}
}

func (e *hubEntry) onIdleTimeout() {
	e.mu.Lock()
	if e.done || len(e.subs) > 0 {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()
	e.stop(io.EOF)
}

func (e *hubEntry) broadcast(frame agentruntime.StreamFrame) {
	e.mu.Lock()
	eventKind := effectiveFrameEventType(frame)
	deltaKind := ""
	isDeltaFrame := false
	switch eventKind {
	case "event_start":
		isDeltaFrame = true
		pType, pID := extractPreviewStartMeta(frame.Data)
		if pID != "" && pType != "" {
			e.previewMap[pID] = pType
		}
		deltaKind = pType
	case "event_delta":
		isDeltaFrame = true
		pID, inlineKind := extractPreviewDeltaMeta(frame.Data)
		if mapped, ok := e.previewMap[pID]; ok {
			deltaKind = mapped
		} else {
			deltaKind = inlineKind
		}
	default:
		if frame.ID != "" {
			delete(e.previewMap, frame.ID)
		}
	}

	subs := make([]*hubSubscriber, 0, len(e.subs))
	for sub := range e.subs {
		subs = append(subs, sub)
	}
	e.mu.Unlock()

	for _, sub := range subs {
		if isDeltaFrame && !sub.acceptsDelta(deltaKind) {
			continue
		}
		select {
		case sub.ch <- frame:
		case <-sub.closed:
		default:
		}
	}
}

func (e *hubEntry) stop(err error) {
	e.removeFromHub()
	e.mu.Lock()
	if e.done {
		e.mu.Unlock()
		return
	}
	e.done = true
	e.doneErr = err
	if e.idleTimer != nil {
		e.idleTimer.Stop()
		e.idleTimer = nil
	}
	cancel := e.cancel
	subs := make([]*hubSubscriber, 0, len(e.subs))
	for sub := range e.subs {
		subs = append(subs, sub)
		delete(e.subs, sub)
	}
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, sub := range subs {
		sub.finish(err)
	}
}

func (e *hubEntry) removeFromHub() {
	e.hub.mu.Lock()
	if current, ok := e.hub.entries[e.key]; ok && current == e {
		delete(e.hub.entries, e.key)
	}
	e.hub.mu.Unlock()
}

func (s *hubSubscriber) acceptsDelta(deltaKind string) bool {
	if len(s.allowedDeltas) == 0 {
		return false
	}
	if deltaKind == "" {
		return true
	}
	_, ok := s.allowedDeltas[deltaKind]
	return ok
}

func (s *hubSubscriber) finish(err error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.closed)
	})
}

func (s *hubSubscriber) Next(ctx context.Context) (agentruntime.StreamFrame, error) {
	select {
	case frame := <-s.ch:
		return frame, nil
	default:
	}
	select {
	case <-ctx.Done():
		_ = s.Close()
		return agentruntime.StreamFrame{}, ctx.Err()
	case frame := <-s.ch:
		return frame, nil
	case <-s.closed:
		select {
		case frame := <-s.ch:
			return frame, nil
		default:
		}
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err == nil {
			return agentruntime.StreamFrame{}, io.EOF
		}
		return agentruntime.StreamFrame{}, err
	}
}

func (s *hubSubscriber) Close() error {
	s.finish(io.EOF)
	s.entry.removeSubscriber(s)
	return nil
}

type sseFrameReader struct {
	scanner *bufio.Scanner
}

func newSSEFrameReader(r io.Reader) *sseFrameReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	return &sseFrameReader{scanner: scanner}
}

func (r *sseFrameReader) readNext() (agentruntime.StreamFrame, error) {
	var (
		id        string
		eventName string
		dataLines []string
		hasField  bool
	)
	for r.scanner.Scan() {
		line := strings.TrimSuffix(r.scanner.Text(), "\r")
		if line == "" {
			if hasField && len(dataLines) > 0 {
				return agentruntime.StreamFrame{
					ID:    id,
					Event: eventName,
					Data:  json.RawMessage(strings.Join(dataLines, "\n")),
				}, nil
			}
			id = ""
			eventName = ""
			dataLines = nil
			hasField = false
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		hasField = true
		switch field {
		case "id":
			id = value
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if err := r.scanner.Err(); err != nil {
		return agentruntime.StreamFrame{}, err
	}
	if hasField && len(dataLines) > 0 {
		return agentruntime.StreamFrame{
			ID:    id,
			Event: eventName,
			Data:  json.RawMessage(strings.Join(dataLines, "\n")),
		}, nil
	}
	return agentruntime.StreamFrame{}, io.EOF
}

func encodeStreamFrame(frame agentruntime.StreamFrame) (agentruntime.StreamFrame, error) {
	encodedData, err := EncodeEventPayload(frame.Data)
	if err != nil {
		return agentruntime.StreamFrame{}, err
	}
	return agentruntime.StreamFrame{
		ID:    EncodeEventID(frame.ID),
		Event: frame.Event,
		Data:  encodedData,
	}, nil
}

func effectiveFrameEventType(frame agentruntime.StreamFrame) string {
	if frame.Event != "" {
		return frame.Event
	}
	var payload struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(frame.Data, &payload)
	return payload.Type
}

func extractPreviewStartMeta(data json.RawMessage) (string, string) {
	var payload struct {
		Event struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"event"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", ""
	}
	return payload.Event.Type, payload.Event.ID
}

func extractPreviewDeltaMeta(data json.RawMessage) (string, string) {
	var payload struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		Delta     struct {
			Type    string `json:"type"`
			Content struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", ""
	}
	kind := payload.EventType
	if kind == "" {
		switch payload.Delta.Content.Type {
		case "thinking":
			kind = "agent.thinking"
		case "text":
			kind = "agent.message"
		}
	}
	return payload.EventID, kind
}

type streamUsageEvent struct {
	Type                  string             `json:"type"`
	ProcessedAt           time.Time          `json:"processed_at"`
	ModelCredits          float64            `json:"model_credits"`
	SandboxRuntimeCredits float64            `json:"sandbox_runtime_credits"`
	TotalCredits          float64            `json:"total_credits"`
	Usage                 *qoderSessionUsage `json:"usage"`
	StopReason            *struct {
		Type string `json:"type"`
	} `json:"stop_reason"`
}

func (r *Runtime) handleStreamSideEffects(
	ctx context.Context,
	scope agentruntime.Scope,
	binding db.RuntimeResourceBinding,
	frame agentruntime.StreamFrame,
) {
	eventType := effectiveFrameEventType(frame)
	switch eventType {
	case "session.usage":
		var payload streamUsageEvent
		if err := json.Unmarshal(frame.Data, &payload); err != nil {
			return
		}
		usage := qoderSessionUsage{
			ModelCredits:          payload.ModelCredits,
			SandboxRuntimeCredits: payload.SandboxRuntimeCredits,
			TotalCredits:          payload.TotalCredits,
		}
		if payload.Usage != nil && usage.TotalCredits == 0 && usage.ModelCredits == 0 && usage.SandboxRuntimeCredits == 0 {
			usage = *payload.Usage
		}
		r.recordSessionUsage(ctx, scope, binding.OMAExternalID, binding.RemoteID, usage, payload.ProcessedAt)
	case "session.status_idle":
		var payload streamUsageEvent
		_ = json.Unmarshal(frame.Data, &payload)
		status := "idle"
		if payload.StopReason != nil && strings.Contains(strings.ToLower(payload.StopReason.Type), "error") {
			status = "failed"
		}
		r.refreshSessionBindingSummary(ctx, scope, binding, "", status, "")
		if payload.Usage != nil {
			r.recordSessionUsage(ctx, scope, binding.OMAExternalID, binding.RemoteID, *payload.Usage, payload.ProcessedAt)
		}
	case "session.status_terminated", "session.error":
		r.refreshSessionBindingSummary(ctx, scope, binding, "", "failed", "")
	}
}
