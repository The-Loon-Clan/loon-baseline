package jobsettings

import (
	"context"
	"testing"

	"github.com/the-loon-clan/loon/schedule"
)

// The Mock must drive a real JobInfo's config exactly as the PGStore would, so
// the test exercises the store *through* the scheduler's DeclareConfig/GetConfig
// path — that's the contract that matters.
func TestMockDrivesJobConfig(t *testing.T) {
	ctx := context.Background()
	store := NewMock()

	// A bare JobInfo (registry defaults to schedule.Default) with two vars.
	job := schedule.RegisterService("Search API", "read tier")
	job.DeclareConfig(store,
		schedule.JobConfigVar{Key: "cache_ttl_secs", Type: schedule.JobConfigInt, Default: "90"},
		schedule.JobConfigVar{Key: "caps_ttl_secs", Type: schedule.JobConfigInt, Default: "3600"},
	)

	// No override yet -> declared default.
	if got := job.GetConfigInt("cache_ttl_secs"); got != 90 {
		t.Fatalf("default cache_ttl_secs = %d, want 90", got)
	}

	// Set an override -> it takes effect (SetConfig refreshes the cache).
	if err := job.SetConfig(ctx, "cache_ttl_secs", "3600"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if got := job.GetConfigInt("cache_ttl_secs"); got != 3600 {
		t.Fatalf("override cache_ttl_secs = %d, want 3600", got)
	}

	// Empty value reverts to the declared default (delete semantics).
	if err := job.SetConfig(ctx, "cache_ttl_secs", ""); err != nil {
		t.Fatalf("SetConfig empty: %v", err)
	}
	if got := job.GetConfigInt("cache_ttl_secs"); got != 90 {
		t.Fatalf("after revert cache_ttl_secs = %d, want 90 (default)", got)
	}

	// The untouched var still reports its default.
	if got := job.GetConfigInt("caps_ttl_secs"); got != 3600 {
		t.Fatalf("caps_ttl_secs = %d, want 3600", got)
	}
}

func TestSetEmptyDeletes(t *testing.T) {
	ctx := context.Background()
	store := NewMock()
	_ = store.SetJobSetting(ctx, "j", "k", "v")
	_ = store.SetJobSetting(ctx, "j", "k", "") // empty -> delete
	m, _ := store.GetJobSettings(ctx, "j")
	if _, ok := m["k"]; ok {
		t.Fatalf("empty value should have deleted the override, got %v", m)
	}
}
