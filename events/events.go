// Package events is loon-baseline's tiny in-process publish/subscribe bus: named
// topics, any number of subscribers, fire-and-forget Emit. It turns one-off
// couplings ("on ingest, clear the search cache") into open hook points any host
// or plugin can subscribe to — the same shape as notify.Fanout, generalized
// beyond notifications.
//
// Emit is matched by a structural interface, so a plugin can publish through the
// core extension registry without importing this package:
//
//	if v, ok := core.Lookup("events"); ok {
//	    if e, ok := v.(interface{ Emit(context.Context, string, any) }); ok {
//	        e.Emit(ctx, "usenet.ingested", payload)
//	    }
//	}
//
// The bus is IN-PROCESS by design (see LOON-DISTRIBUTED): a cross-process effect
// happens through shared state a subscriber touches — e.g. a subscriber in the
// worker DELETEs the shared Redis cache the API tier reads — not by shipping the
// event over the wire. Publishing an unknown topic is a no-op; a publisher never
// needs a subscriber to exist.
package events

import (
	"context"
	"sync"
)

// Handler receives one published payload. Emit calls handlers inline, so keep
// them quick or hand off to a goroutine.
type Handler = func(ctx context.Context, payload any)

// Bus maps topics to their subscribers.
type Bus struct {
	mu   sync.RWMutex
	subs map[string][]Handler
}

func NewBus() *Bus { return &Bus{subs: map[string][]Handler{}} }

// Subscribe registers h for topic. Call at wiring time (before events fire).
func (b *Bus) Subscribe(topic string, h Handler) {
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], h)
	b.mu.Unlock()
}

// Emit delivers payload to every subscriber of topic, inline and in registration
// order. Best-effort: it's the subscriber's job to be quick and not panic (an
// emit from a background job is already under the job's panic recovery).
func (b *Bus) Emit(ctx context.Context, topic string, payload any) {
	b.mu.RLock()
	hs := append([]Handler(nil), b.subs[topic]...)
	b.mu.RUnlock()
	for _, h := range hs {
		h(ctx, payload)
	}
}
