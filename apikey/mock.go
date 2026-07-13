package apikey

import (
	"context"
	"sync"
	"time"
)

// Mock is a concurrency-safe in-memory Store for tests and no-database hosts.
type Mock struct {
	mu     sync.Mutex
	byUser map[int64]Key
	byKey  map[string]int64
}

func NewMock() *Mock {
	return &Mock{byUser: map[int64]Key{}, byKey: map[string]int64{}}
}

var _ Store = (*Mock)(nil)

func (m *Mock) Resolve(_ context.Context, key string) (int64, bool, error) {
	if key == "" {
		return 0, false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	uid, ok := m.byKey[key]
	return uid, ok, nil
}

func (m *Mock) Ensure(_ context.Context, userID int64) (Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.byUser[userID]; ok {
		return k, nil
	}
	gen, err := Generate()
	if err != nil {
		return Key{}, err
	}
	k := Key{UserID: userID, Key: gen, CreatedAt: time.Now()}
	m.byUser[userID] = k
	m.byKey[gen] = userID
	return k, nil
}

func (m *Mock) Rotate(_ context.Context, userID int64) (Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.byUser[userID]; ok {
		delete(m.byKey, old.Key)
	}
	gen, err := Generate()
	if err != nil {
		return Key{}, err
	}
	k := Key{UserID: userID, Key: gen, RotatedAt: time.Now(), CreatedAt: time.Now()}
	m.byUser[userID] = k
	m.byKey[gen] = userID
	return k, nil
}
