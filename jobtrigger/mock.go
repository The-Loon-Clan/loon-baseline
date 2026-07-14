package jobtrigger

import (
	"context"
	"sync"
)

// Mock is an in-memory queue for tests and no-database hosts. Deduped + FIFO.
type Mock struct {
	mu      sync.Mutex
	pending []string
}

func NewMock() *Mock { return &Mock{} }

var _ Store = (*Mock)(nil)

func (m *Mock) Request(_ context.Context, jobName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.pending {
		if n == jobName {
			return nil // already pending
		}
	}
	m.pending = append(m.pending, jobName)
	return nil
}

func (m *Mock) Claim(_ context.Context, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.pending) {
		limit = len(m.pending)
	}
	out := append([]string(nil), m.pending[:limit]...)
	m.pending = m.pending[limit:]
	return out, nil
}
