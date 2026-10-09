package agents

import (
	"context"
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

// Label returns the provider's display label, falling back to its ID.
func Label(p Provider) string {
	if d, ok := p.(Describer); ok {
		if label := d.Label(); label != "" {
			return label
		}
	}
	return string(p.ID())
}

// DescribeAccount returns display-safe account fields, or a zero value when the
// account's provider is unregistered or has nothing to describe.
func (r *Registry) DescribeAccount(ctx context.Context, account Account) AccountInfo {
	p, err := r.Get(account.Provider)
	if err != nil {
		return AccountInfo{}
	}
	if d, ok := p.(AccountDescriber); ok {
		return d.DescribeAccount(ctx, account)
	}
	return AccountInfo{}
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
