package agentruntime

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type ProfileRuntimeConfig struct {
	Profile      db.RuntimeProfile
	Credentials  []byte
	Capabilities CapabilitySet
}

type Factory func(ctx context.Context, cfg ProfileRuntimeConfig) (Runtime, error)

type Registry struct {
	mu        sync.RWMutex
	factories map[ProviderKind]Factory
}

func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[ProviderKind]Factory),
	}
}

func (r *Registry) Register(kind ProviderKind, factory Factory) error {
	if kind == "" || kind.IsNative() || factory == nil {
		return InvalidArgumentError("invalid runtime provider registration", ErrUnsupportedProvider)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[kind]; exists {
		return InvalidArgumentError("runtime provider already registered", fmt.Errorf("%w: %s", ErrUnsupportedProvider, kind))
	}
	r.factories[kind] = factory
	return nil
}

func (r *Registry) Has(kind ProviderKind) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[kind]
	return ok
}

func (r *Registry) IsValidProviderKind(kind ProviderKind) bool {
	if kind == "" {
		return false
	}
	if kind.IsNative() || kind == ProviderQoderCloud {
		return true
	}
	return r.Has(kind)
}

func (r *Registry) SupportedProviders() []ProviderKind {
	providers := []ProviderKind{ProviderOMANative}
	if r == nil {
		return providers
	}
	r.mu.RLock()
	for kind := range r.factories {
		providers = append(providers, kind)
	}
	r.mu.RUnlock()
	slices.Sort(providers)
	return providers
}

func (r *Registry) Build(ctx context.Context, cfg ProfileRuntimeConfig) (Runtime, error) {
	kind := ProviderKind(cfg.Profile.ProviderKind)
	if kind == "" || kind.IsNative() {
		return nil, InvalidArgumentError("cannot build external runtime for native or empty provider", ErrUnsupportedProvider)
	}
	if r == nil {
		return nil, UnavailableError("runtime provider is not registered", fmt.Errorf("%w: %s", ErrProviderNotRegistered, kind))
	}
	r.mu.RLock()
	factory, ok := r.factories[kind]
	r.mu.RUnlock()
	if !ok {
		return nil, UnavailableError("runtime provider is not registered", fmt.Errorf("%w: %s", ErrProviderNotRegistered, kind))
	}
	return factory(ctx, cfg)
}
