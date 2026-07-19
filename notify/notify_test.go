package notify

import (
	"context"
	"testing"

	"github.com/the-loon-clan/loon/core"
)

func TestFanoutDeliversToEverySink(t *testing.T) {
	ctx := context.Background()
	var a, b int
	f := NewFanout(func(context.Context, int64, core.Notification) error { a++; return nil })
	f.Add(func(context.Context, int64, core.Notification) error { b++; return nil })
	_ = f.Deliver(ctx, 1, core.Notification{Title: "hi"})
	if a != 1 || b != 1 {
		t.Fatalf("not all sinks called: a=%d b=%d", a, b)
	}
}

func TestFanoutSkipsSelfNotifications(t *testing.T) {
	ctx := context.Background()
	called := 0
	f := NewFanout(func(context.Context, int64, core.Notification) error { called++; return nil })
	_ = f.Deliver(ctx, 5, core.Notification{Title: "x", ActorID: 5}) // actor==recipient → skip
	if called != 0 {
		t.Fatal("self-notification should be skipped")
	}
	_ = f.Deliver(ctx, 5, core.Notification{Title: "x", ActorID: 6}) // cross-user → deliver
	_ = f.Deliver(ctx, 5, core.Notification{Title: "x"})             // system (ActorID 0) → deliver
	if called != 2 {
		t.Fatalf("cross-user + system should deliver, called=%d", called)
	}
}

// memInbox is an in-memory InboxStore for the sink test.
type memInbox struct{ items map[int64][]Item }

func newMemInbox() *memInbox { return &memInbox{items: map[int64][]Item{}} }

var _ InboxStore = (*memInbox)(nil)

func (m *memInbox) Add(_ context.Context, uid int64, n core.Notification) error {
	m.items[uid] = append(m.items[uid], Item{Title: n.Title, Body: n.Body})
	return nil
}
func (m *memInbox) List(_ context.Context, uid int64, _ int) ([]Item, error) { return m.items[uid], nil }
func (m *memInbox) UnreadCount(_ context.Context, uid int64) (int, error) {
	n := 0
	for _, it := range m.items[uid] {
		if !it.Read {
			n++
		}
	}
	return n, nil
}
func (m *memInbox) MarkAllRead(_ context.Context, uid int64) error {
	for i := range m.items[uid] {
		m.items[uid][i].Read = true
	}
	return nil
}

func TestInboxSinkStoresAndMarksRead(t *testing.T) {
	ctx := context.Background()
	inbox := newMemInbox()
	f := NewFanout(InboxSink(inbox))
	_ = f.Deliver(ctx, 1, core.Notification{Title: "welcome"})
	if c, _ := inbox.UnreadCount(ctx, 1); c != 1 {
		t.Fatalf("unread = %d, want 1", c)
	}
	_ = inbox.MarkAllRead(ctx, 1)
	if c, _ := inbox.UnreadCount(ctx, 1); c != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", c)
	}
}
