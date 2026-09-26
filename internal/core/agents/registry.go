package agents

import (
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[ProviderID]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[ProviderID]Provider)}
}

func (r *Registry) Register(p Provider) error {
	if p == nil || p.ID() == "" {
		return fmt.Errorf("register provider: empty provider id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[p.ID()]; ok {
		return fmt.Errorf("register provider %q: already registered", p.ID())
	}
	r.providers[p.ID()] = p
	return nil
}

func (r *Registry) Get(id ProviderID) (Provider, error) {
	r.mu.RLock()
	p, ok := r.providers[id]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
	}
	return p, nil
}

func (r *Registry) List() []Provider {
	r.mu.RLock()
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
