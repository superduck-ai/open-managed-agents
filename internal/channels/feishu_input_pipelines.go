package channels

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/larksuite/oapi-sdk-go/v3/channel/pipeline"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type feishuInputPipelines struct {
	mu     sync.Mutex
	config channeltypes.BatchConfig
	scopes map[string]*pipeline.ChatPipeline
}

func newFeishuInputPipelines(config channeltypes.BatchConfig) *feishuInputPipelines {
	return &feishuInputPipelines{config: config, scopes: make(map[string]*pipeline.ChatPipeline)}
}

func (p *feishuInputPipelines) scope(key string) *pipeline.ChatPipeline {
	p.mu.Lock()
	defer p.mu.Unlock()
	queue := p.scopes[key]
	if queue == nil {
		queue = pipeline.NewChatPipeline(p.config, false)
		p.scopes[key] = queue
	}
	return queue
}

func (p *feishuInputPipelines) FlushAll(ctx context.Context) {
	p.mu.Lock()
	queues := slices.Collect(maps.Values(p.scopes))
	p.mu.Unlock()
	for _, queue := range queues {
		queue.FlushNow(ctx)
	}
}

func (p *feishuInputPipelines) Dispose() {
	p.FlushAll(context.Background())
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, queue := range p.scopes {
		queue.Dispose()
	}
	clear(p.scopes)
}
