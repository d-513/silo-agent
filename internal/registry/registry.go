// Package registry is the ordered, id-keyed list the pluggable families share:
// model providers, search engines, built-in connectors. Each entry pairs the
// descriptor a UI lists with the factory (or implementation) behind it.
package registry

import "sync"

type entry[D, F any] struct {
	desc D
	impl F
}

// Registry keeps entries in registration order and is safe for concurrent use.
type Registry[D, F any] struct {
	mu   sync.RWMutex
	id   func(D) string
	list []entry[D, F]
}

// New makes an empty registry; id names a descriptor's key.
func New[D, F any](id func(D) string) *Registry[D, F] {
	return &Registry[D, F]{id: id}
}

// Put adds the entry, or replaces the one with the same id in place so its
// position in the order is kept.
func (r *Registry[D, F]) Put(d D, impl F) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.list {
		if r.id(e.desc) == r.id(d) {
			r.list[i] = entry[D, F]{d, impl}
			return
		}
	}
	r.list = append(r.list, entry[D, F]{d, impl})
}

// Remove drops the entry with that id, if there is one.
func (r *Registry[D, F]) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]entry[D, F], 0, len(r.list))
	for _, e := range r.list {
		if r.id(e.desc) != id {
			out = append(out, e)
		}
	}
	r.list = out
}

// Lookup finds an entry by id.
func (r *Registry[D, F]) Lookup(id string) (D, F, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.list {
		if r.id(e.desc) == id {
			return e.desc, e.impl, true
		}
	}
	var d D
	var f F
	return d, f, false
}

// All returns the descriptors in registration order, as a copy.
func (r *Registry[D, F]) All() []D {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]D, len(r.list))
	for i, e := range r.list {
		out[i] = e.desc
	}
	return out
}
