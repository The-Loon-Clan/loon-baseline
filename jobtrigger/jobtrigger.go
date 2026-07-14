// Package jobtrigger is the cross-process "run now" queue behind loon's
// schedule.RemoteTrigger hook. A web/coordinator process enqueues a run request
// for a job whose loop lives on a worker (a schedule.MarkRemote stub); the
// worker polls, claims, and runs it locally via schedule.TriggerJob. It's
// durable (survives a restart) and multi-worker-safe (a claim skips locked
// rows), with no message bus — just a small table, matching LOON-DISTRIBUTED
// (coordinate through shared state, not a wire bus).
//
// Wiring, split deployment:
//
//	web:    schedule.RemoteTrigger = func(n string) error { return store.Request(ctx, n) }
//	worker: go jobtrigger.StartPoller(ctx, store, 3*time.Second, schedule.TriggerJob)
//
// Never both in one process — a triggerless job would re-enqueue itself.
package jobtrigger

import (
	"context"
	"time"
)

// Store is the durable run-request queue.
type Store interface {
	// Request enqueues a run for jobName, deduped: a second request while one is
	// still pending is a no-op.
	Request(ctx context.Context, jobName string) error
	// Claim atomically removes up to limit pending requests and returns their job
	// names (oldest first). Safe for concurrent workers — locked rows are skipped.
	Claim(ctx context.Context, limit int) ([]string, error)
}

// StartPoller drains the queue every interval until ctx is done, calling run for
// each claimed job name (pass schedule.TriggerJob). Run in a goroutine on the
// WORKER process only. A Claim error is skipped and retried next tick.
func StartPoller(ctx context.Context, store Store, interval time.Duration, run func(jobName string) bool) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			names, err := store.Claim(ctx, 32)
			if err != nil {
				continue
			}
			for _, n := range names {
				run(n)
			}
		}
	}
}
