package playergen

import "sync"

// NameRegistry prevents duplicate (first, last) combinations within a scope
// (one registry per world or team). It is purely in-memory; nothing about it is
// persisted in S01-04. The per-nationality-pool collision check required for
// player generation is keyed on (nationality code, first name, last name).
type NameRegistry struct {
	mu   sync.Mutex
	used map[string]map[string]struct{}
}

// NewNameRegistry returns an empty registry.
func NewNameRegistry() *NameRegistry {
	return &NameRegistry{used: make(map[string]map[string]struct{})}
}

// Reserve claims (first, last) for a nationality code, returning true when the
// registration is new and false when it is already taken.
func (r *NameRegistry) Reserve(code, first, last string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	names, ok := r.used[code]
	if !ok {
		names = make(map[string]struct{})
		r.used[code] = names
	}
	key := first + "\x00" + last
	if _, dup := names[key]; dup {
		return false
	}
	names[key] = struct{}{}
	return true
}

// Release frees a prior claim.
func (r *NameRegistry) Release(code, first, last string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	names, ok := r.used[code]
	if !ok {
		return
	}
	delete(names, first+"\x00"+last)
}
