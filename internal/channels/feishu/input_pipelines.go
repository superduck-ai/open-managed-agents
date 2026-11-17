package feishu

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/larksuite/oapi-sdk-go/v3/channel/pipeline"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type InputPipelines struct {
	mu     sync.Mutex
	config channeltypes.BatchConfig
	scopes map[string]*pipeline.ChatPipeline
}

func NewInputPipelines(config channeltypes.BatchConfig) *InputPipelines {
	return &InputPipelines{config: config, scopes: make(map[string]*pipeline.ChatPipeline)}
}

func (p *InputPipelines) scope(key string) *pipeline.ChatPipeline {
	p.mu.Lock()
	defer p.mu.Unlock()
	queue := p.scopes[key]
	if queue == nil {
		queue = pipeline.NewChatPipeline(p.config, false)
		p.scopes[key] = queue
	}
	return queue
}

func (p *InputPipelines) FlushAll(ctx context.Context) {
	p.mu.Lock()
	queues := slices.Collect(maps.Values(p.scopes))
	p.mu.Unlock()
	for _, queue := range queues {
		queue.FlushNow(ctx)
	}
}

func (p *InputPipelines) Dispose() {
	p.FlushAll(context.Background())
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, queue := range p.scopes {
		queue.Dispose()
	}
	clear(p.scopes)
}
