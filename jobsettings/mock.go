package jobsettings

import (
	"context"
	"sync"

	"github.com/the-loon-clan/loon/schedule"
)

// Mock is a concurrency-safe in-memory Store for tests and no-database hosts.
// Config values are read from many job-loop goroutines and written rarely, so
// a single mutex is fine.
type Mock struct {
	mu sync.RWMutex
	m  map[string]map[string]string // jobName -> key -> value
}

func NewMock() *Mock { return &Mock{m: map[string]map[string]string{}} }

var (
	_ Store                   = (*Mock)(nil)
	_ schedule.JobConfigStore = (*Mock)(nil)
)

func (mk *Mock) GetJobSettings(_ context.Context, jobName string) (map[string]string, error) {
	mk.mu.RLock()
	defer mk.mu.RUnlock()
	out := map[string]string{}
	for k, v := range mk.m[jobName] {
		out[k] = v
	}
	return out, nil
}

func (mk *Mock) SetJobSetting(_ context.Context, jobName, key, value string) error {
	mk.mu.Lock()
	defer mk.mu.Unlock()
	if value == "" {
		delete(mk.m[jobName], key)
		return nil
	}
	if mk.m[jobName] == nil {
		mk.m[jobName] = map[string]string{}
	}
	mk.m[jobName][key] = value
	return nil
}

func (mk *Mock) DeleteJobSetting(_ context.Context, jobName, key string) error {
	mk.mu.Lock()
	defer mk.mu.Unlock()
	delete(mk.m[jobName], key)
	return nil
}
