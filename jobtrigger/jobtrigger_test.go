package jobtrigger

import (
	"context"
	"testing"

	"github.com/ameNZB/loon/schedule"
)

func TestMockDedupAndClaimFIFO(t *testing.T) {
	ctx := context.Background()
	m := NewMock()
	_ = m.Request(ctx, "A")
	_ = m.Request(ctx, "B")
	_ = m.Request(ctx, "A") // dedup while pending

	names, _ := m.Claim(ctx, 10)
	if len(names) != 2 || names[0] != "A" || names[1] != "B" {
		t.Fatalf("claim = %v, want [A B] (deduped, FIFO)", names)
	}
	// Claim again: empty.
	if again, _ := m.Claim(ctx, 10); len(again) != 0 {
		t.Fatalf("second claim should be empty, got %v", again)
	}
	// A can be requested again once it's no longer pending.
	_ = m.Request(ctx, "A")
	if names, _ := m.Claim(ctx, 10); len(names) != 1 || names[0] != "A" {
		t.Fatalf("re-request after claim = %v, want [A]", names)
	}
}

// The whole cross-process loop, in one test with two registries + the global
// RemoteTrigger hook: a web-side "run now" on a remote stub enqueues, a
// worker-side drain claims + runs the real job, and repeated clicks collapse to
// one run.
func TestFullLoopThroughScheduler(t *testing.T) {
	ctx := context.Background()
	store := NewMock()
	schedule.RemoteTrigger = func(name string) error { return store.Request(ctx, name) }
	defer func() { schedule.RemoteTrigger = nil }()

	// Web process: a MarkRemote stub (no local trigger).
	web := schedule.NewRegistry()
	web.RegisterJob("Crawl", "").MarkRemote()

	// Worker process: the real job with a local trigger.
	worker := schedule.NewRegistry()
	ran := 0
	worker.RegisterJob("Crawl", "").SetTrigger(func() { ran++ })

	// "Run now" clicked twice on the web admin -> one pending request.
	if !web.TriggerJob("Crawl") {
		t.Fatal("web trigger should report queued")
	}
	web.TriggerJob("Crawl")

	// Worker drains + runs locally.
	names, _ := store.Claim(ctx, 32)
	for _, n := range names {
		worker.TriggerJob(n)
	}

	if ran != 1 {
		t.Fatalf("job ran %d times, want 1 (deduped run-now)", ran)
	}
}
