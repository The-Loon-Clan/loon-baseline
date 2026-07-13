package apikey

import (
	"context"
	"testing"
)

// Ensure + Resolve + Rotate must round-trip, and a rotation must invalidate the
// old key immediately. Runs against the Mock (the PGStore shares the same
// contract; integration against Postgres is the host's job).
func TestKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	m := NewMock()

	// Ensure creates a key on first call and is idempotent after.
	k1, err := m.Ensure(ctx, 42)
	if err != nil || k1.Key == "" {
		t.Fatalf("Ensure: key=%q err=%v", k1.Key, err)
	}
	if len(k1.Key) != 64 {
		t.Fatalf("key should be 256-bit hex (64 chars), got %d", len(k1.Key))
	}
	k1again, _ := m.Ensure(ctx, 42)
	if k1again.Key != k1.Key {
		t.Fatalf("Ensure not idempotent: %q vs %q", k1again.Key, k1.Key)
	}

	// Resolve maps the key back to its user; a bogus key doesn't resolve.
	if uid, ok, _ := m.Resolve(ctx, k1.Key); !ok || uid != 42 {
		t.Fatalf("Resolve = (%d,%v), want (42,true)", uid, ok)
	}
	if _, ok, _ := m.Resolve(ctx, "nope"); ok {
		t.Fatalf("bogus key should not resolve")
	}
	if _, ok, _ := m.Resolve(ctx, ""); ok {
		t.Fatalf("empty key should not resolve")
	}

	// Rotate (refresh) issues a new key AND the old one stops resolving.
	k2, err := m.Rotate(ctx, 42)
	if err != nil || k2.Key == k1.Key {
		t.Fatalf("Rotate should mint a different key: old=%q new=%q err=%v", k1.Key, k2.Key, err)
	}
	if k2.RotatedAt.IsZero() {
		t.Fatalf("Rotate should stamp RotatedAt")
	}
	if _, ok, _ := m.Resolve(ctx, k1.Key); ok {
		t.Fatalf("old key must stop resolving after rotate")
	}
	if uid, ok, _ := m.Resolve(ctx, k2.Key); !ok || uid != 42 {
		t.Fatalf("new key should resolve to 42, got (%d,%v)", uid, ok)
	}
}
