package events

import (
	"context"
	"testing"
)

func TestEmitReachesTopicSubscribers(t *testing.T) {
	b := NewBus()
	var got []string
	b.Subscribe("ingested", func(_ context.Context, p any) { got = append(got, "a:"+p.(string)) })
	b.Subscribe("ingested", func(_ context.Context, p any) { got = append(got, "b:"+p.(string)) })
	b.Subscribe("other", func(_ context.Context, _ any) { got = append(got, "other") })

	b.Emit(context.Background(), "ingested", "x")

	// Both subscribers of the topic fire, in order; the other topic doesn't.
	if len(got) != 2 || got[0] != "a:x" || got[1] != "b:x" {
		t.Fatalf("subscribers = %v, want [a:x b:x]", got)
	}
}

func TestEmitUnknownTopicIsNoop(t *testing.T) {
	b := NewBus()
	b.Emit(context.Background(), "nobody-listening", 42) // must not panic
}

// A *Bus satisfies the structural interface a plugin uses to publish through the
// core extension registry without importing this package.
func TestBusSatisfiesEmitterInterface(t *testing.T) {
	var _ interface {
		Emit(context.Context, string, any)
	} = NewBus()
}
