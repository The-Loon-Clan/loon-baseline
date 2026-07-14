package memory

import (
	"context"
	"testing"
)

func TestDeletePrefix(t *testing.T) {
	ctx := context.Background()
	c := New()
	_ = c.Set(ctx, "newznab:v1:aaa", []byte("1"), 0)
	_ = c.Set(ctx, "newznab:v1:bbb", []byte("2"), 0)
	_ = c.Set(ctx, "other:zzz", []byte("3"), 0)

	if err := c.DeletePrefix(ctx, "newznab:v1:"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}

	// Both namespaced keys gone; the unrelated key survives.
	if _, ok, _ := c.Get(ctx, "newznab:v1:aaa"); ok {
		t.Fatal("newznab:v1:aaa should be gone")
	}
	if _, ok, _ := c.Get(ctx, "newznab:v1:bbb"); ok {
		t.Fatal("newznab:v1:bbb should be gone")
	}
	if _, ok, _ := c.Get(ctx, "other:zzz"); !ok {
		t.Fatal("other:zzz should survive a scoped flush")
	}
}
