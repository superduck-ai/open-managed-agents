package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type blockingMCPCatalogEnqueuer struct {
	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

func (e *blockingMCPCatalogEnqueuer) EnsureAgent(context.Context, int64, json.RawMessage, string) error {
	close(e.started)
	<-e.release
	close(e.done)
	return nil
}

func TestEnqueueMCPCatalogDoesNotWaitForScheduling(t *testing.T) {
	enqueuer := &blockingMCPCatalogEnqueuer{
		started: make(chan struct{}),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	handler := &Handler{mcp: enqueuer}

	returned := make(chan struct{})
	go func() {
		handler.enqueueMCPCatalog(
			context.Background(),
			2,
			json.RawMessage(`[{"name":"weather","url":"https://weather.example/mcp"}]`),
			"agent_test",
			"agent_create",
		)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(time.Second):
		close(enqueuer.release)
		t.Fatal("enqueueMCPCatalog blocked the Agent response path")
	}
	select {
	case <-enqueuer.started:
	case <-time.After(time.Second):
		close(enqueuer.release)
		t.Fatal("background MCP catalog scheduling did not start")
	}

	close(enqueuer.release)
	select {
	case <-enqueuer.done:
	case <-time.After(time.Second):
		t.Fatal("background MCP catalog scheduling did not finish")
	}
}
