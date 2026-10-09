package agentruntime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"sync"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type nativeWorker interface {
	Next(context.Context) (codesessions.HostInput, error)
	Acknowledge(context.Context, string, string) error
	Heartbeat(context.Context) error
	SetState(context.Context, string) error
	EndTurn(context.Context, bool) error
	Publish(context.Context, json.RawMessage) error
	Close(context.Context) error
	LoadHistory(context.Context) ([]codesessions.HostHistoryEntry, error)
	AppendHistory(context.Context, []codesessions.HostHistoryEntry) error
	RequestPermission(context.Context, codesessions.HostToolRequest) (codesessions.HostToolPermission, error)
}

type Service struct {
	sessions     *codesessions.Service
	modelBaseURL string
	logger       *slog.Logger
	mu           sync.Mutex
	active       map[string]*execution
}

type execution struct {
	service       *Service
	worker        nativeWorker
	codeSessionID string
	epoch         int64
	cancel        context.CancelFunc
	model         fantasy.LanguageModel
	configuration launchConfig
	connections   []*mcpConnection
	permissionMu  sync.Mutex
	permissions   map[string]chan permissionResponse
	turnCancel    context.CancelFunc
	pending       []codesessions.HostInput
	ready         chan struct{}
	done          chan struct{}
	startErr      error
	children      sync.WaitGroup
}

func New(sessions *codesessions.Service, serverAddress string, logger *slog.Logger) (*Service, error) {
	host, port, err := net.SplitHostPort(serverAddress)
	if err != nil || port == "" || sessions == nil {
		return nil, ErrInvalidConfig
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return &Service{sessions: sessions, modelBaseURL: "http://" + net.JoinHostPort(host, port), logger: logging.LoggerOrDefault(logger), active: make(map[string]*execution)}, nil
}

func (s *Service) Start(ctx context.Context, input StartInput) error {
	var configuration launchConfig
	if err := json.Unmarshal(input.SessionConfig, &configuration); err != nil || configuration.Mode != Host || configuration.Model == "" || input.WorkerEpoch <= 0 || input.MCPToken == "" || input.OAuthAccessToken == "" {
		return ErrInvalidConfig
	}
	s.mu.Lock()
	if prior := s.active[input.CodeSessionID]; prior != nil {
		if prior.epoch == input.WorkerEpoch {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-prior.ready:
				return prior.startErr
			}
		}
		prior.cancel()
	}
	runCtx, cancel := context.WithCancel(ctx)
	current := &execution{service: s, codeSessionID: input.CodeSessionID, epoch: input.WorkerEpoch, configuration: configuration, cancel: cancel, permissions: make(map[string]chan permissionResponse), ready: make(chan struct{}), done: make(chan struct{})}
	s.active[input.CodeSessionID] = current
	s.mu.Unlock()
	err := current.initialize(runCtx, input)
	s.mu.Lock()
	if err == nil && s.active[input.CodeSessionID] != current {
		err = context.Canceled
	}
	current.startErr = err
	close(current.ready)
	if err != nil {
		if s.active[input.CodeSessionID] == current {
			delete(s.active, input.CodeSessionID)
		}
		s.mu.Unlock()
		current.cancel()
		current.closeConnections()
		if current.worker != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = current.worker.Close(cleanupCtx)
			cleanupCancel()
		}
		close(current.done)
		return err
	}
	s.mu.Unlock()
	go current.run(runCtx)
	return nil
}

func (e *execution) initialize(ctx context.Context, input StartInput) error {
	if err := e.connect(ctx, input); err != nil {
		return err
	}
	for _, connection := range e.connections {
		if _, err := connection.agentTools(nil); err != nil {
			return err
		}
	}
	worker, err := e.service.sessions.OpenHostWorker(ctx, input.CodeSessionID, input.WorkerEpoch)
	if err != nil {
		return err
	}
	e.worker = worker
	return nil
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	active := make([]*execution, 0, len(s.active))
	for _, current := range s.active {
		current.cancel()
		active = append(active, current)
	}
	s.mu.Unlock()
	for _, current := range active {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-current.done:
		}
	}
	return nil
}

func (s *Service) Stop(codeSessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.active[codeSessionID]; current != nil {
		current.cancel()
	}
}

func (e *execution) connect(ctx context.Context, input StartInput) error {
	servers, err := gatewayServers(e.configuration)
	if err != nil {
		return err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		attemptCtx, attemptCancel := context.WithTimeout(readyCtx, 3*time.Second)
		connection, err := connectMCP(attemptCtx, input.MCPEndpoint, map[string]string{"Authorization": "Bearer " + input.MCPToken})
		attemptCancel()
		if err == nil {
			connection.remoteServers = servers
			e.connections = append(e.connections, connection)
			break
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-readyCtx.Done():
			timer.Stop()
			return ErrMCPUnavailable
		case <-timer.C:
		}
	}
	provider, err := anthropic.New(anthropic.WithBaseURL(e.service.modelBaseURL), anthropic.WithSkipAuth(true), anthropic.WithHeaders(map[string]string{"Authorization": "Bearer " + input.OAuthAccessToken}))
	if err != nil {
		return ErrInvalidConfig
	}
	e.model, err = provider.LanguageModel(ctx, e.configuration.Model)
	if err != nil {
		return ErrInvalidConfig
	}
	return nil
}

func (e *execution) closeConnections() {
	for _, connection := range e.connections {
		connection.close()
	}
}
