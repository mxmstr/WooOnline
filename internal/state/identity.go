package state

import "sync"

type IdentityRegistry struct {
	mu             sync.RWMutex
	nextConnection uint32
	byAddress      map[string]uint32
}

func NewIdentityRegistry() *IdentityRegistry {
	return &IdentityRegistry{nextConnection: 1, byAddress: make(map[string]uint32)}
}

func (r *IdentityRegistry) NextConnectionID() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.nextConnection
	r.nextConnection++
	return value
}

func (r *IdentityRegistry) SetAuthenticatedPID(address string, pid uint32) {
	r.mu.Lock()
	r.byAddress[address] = pid
	r.mu.Unlock()
}

func (r *IdentityRegistry) AuthenticatedPID(address string) uint32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byAddress[address]
}
