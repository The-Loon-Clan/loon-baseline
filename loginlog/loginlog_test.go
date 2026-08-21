package loginlog

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct{ recorded []Entry }

func (f *fakeStore) Record(_ context.Context, e Entry) error {
	f.recorded = append(f.recorded, e)
	return nil
}
func (f *fakeStore) Recent(context.Context, int64, int) ([]Entry, error) { return nil, nil }
func (f *fakeStore) RecentAll(context.Context, int) ([]Entry, error)     { return nil, nil }

func resolver(known map[string]int64) Resolver {
	return func(_ context.Context, name string) (int64, error) {
		if id, ok := known[name]; ok {
			return id, nil
		}
		return 0, errors.New("not found")
	}
}

func TestAttempt(t *testing.T) {
	ctx := context.Background()
	known := map[string]int64{"alice": 1}
	last := func(fs *fakeStore) Entry { return fs.recorded[len(fs.recorded)-1] }

	// success: uses the passed userID and hashes the IP (never stores it raw)
	fs := &fakeStore{}
	_ = Attempt(ctx, fs, resolver(known), "salt", "1.2.3.4", "alice", 1, true)
	if e := last(fs); e.UserID != 1 || !e.Success || e.Username != "alice" {
		t.Fatalf("success entry = %+v", e)
	}
	if e := last(fs); e.IPHash == "" || e.IPHash == "1.2.3.4" {
		t.Fatalf("IP not hashed: %q", e.IPHash)
	}

	// failed attempt on a KNOWN username: attributed to that account
	fs = &fakeStore{}
	_ = Attempt(ctx, fs, resolver(known), "salt", "1.2.3.4", "alice", 0, false)
	if e := last(fs); e.UserID != 1 || e.Success {
		t.Fatalf("failed-known entry = %+v (want uid 1, success false)", e)
	}

	// failed attempt on an UNKNOWN username: stays unattributed
	fs = &fakeStore{}
	_ = Attempt(ctx, fs, resolver(known), "salt", "1.2.3.4", "ghost", 0, false)
	if e := last(fs); e.UserID != 0 {
		t.Fatalf("unknown username attributed: %+v", e)
	}

	// nil resolver: no attribution attempted
	fs = &fakeStore{}
	_ = Attempt(ctx, fs, nil, "salt", "1.2.3.4", "alice", 0, false)
	if e := last(fs); e.UserID != 0 {
		t.Fatalf("nil resolver attributed: %+v", e)
	}

	// empty IP hashes to empty (no salt-only digest)
	fs = &fakeStore{}
	_ = Attempt(ctx, fs, nil, "salt", "", "x", 0, false)
	if e := last(fs); e.IPHash != "" {
		t.Fatalf("empty IP should stay empty, got %q", e.IPHash)
	}
}
