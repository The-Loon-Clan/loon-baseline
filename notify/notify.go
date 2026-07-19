// Package notify is loon-baseline's notification fan-out + inbox. loon/core owns
// the Notify FACADE (one NotifyFn callback); this package turns that single
// callback into a HOOK point: a Fanout delivers each notification to every
// registered Sink (channel) — a bell/inbox store, a logger, email, Discord, a
// websocket push — and any system (host or plugin) can Add its own.
//
// Sink is a type ALIAS, so a plugin can register a delivery channel via a
// structural interface { Add(func(context.Context, int64, core.Notification) error) }
// off the extension registry, without importing this package.
package notify

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"
)

//go:embed templates/*.html
var viewFS embed.FS

// Sink is a delivery channel: a callback invoked once per notification.
type Sink = func(ctx context.Context, userID int64, n core.Notification) error

// Fanout delivers each notification to every registered Sink, best-effort (one
// failing channel never blocks the others). Pass Deliver as core's NotifyFn.
type Fanout struct {
	mu    sync.RWMutex
	sinks []Sink
}

// NewFanout builds a fan-out over the given initial sinks.
func NewFanout(sinks ...Sink) *Fanout { return &Fanout{sinks: sinks} }

// Add registers another delivery channel. Safe to call from plugin Provision
// (before any notification fires).
func (f *Fanout) Add(s Sink) {
	f.mu.Lock()
	f.sinks = append(f.sinks, s)
	f.mu.Unlock()
}

// Deliver fans a notification out to every sink. Matches
// core.NotificationsAdapter.NotifyFn. A self-notification (ActorID == the
// recipient) is skipped — you don't get pinged about your own actions.
func (f *Fanout) Deliver(ctx context.Context, userID int64, n core.Notification) error {
	if n.ActorID != 0 && n.ActorID == userID {
		return nil
	}
	f.mu.RLock()
	sinks := append([]Sink(nil), f.sinks...)
	f.mu.RUnlock()
	for _, s := range sinks {
		_ = s(ctx, userID, n) // best-effort; each sink handles its own errors
	}
	return nil
}

// LogSink returns a Sink that hands each notification to log (dev/demo).
func LogSink(log func(userID int64, n core.Notification)) Sink {
	return func(_ context.Context, userID int64, n core.Notification) error {
		log(userID, n)
		return nil
	}
}

// InboxSink returns a Sink that stores each notification in the bell/inbox.
func InboxSink(store InboxStore) Sink {
	return func(ctx context.Context, userID int64, n core.Notification) error {
		return store.Add(ctx, userID, n)
	}
}

// Item is one stored notification.
type Item struct {
	ID        int64
	Kind      string
	Title     string
	Body      string
	Link      string
	ActorName string
	Read      bool
	CreatedAt time.Time
}

// InboxStore persists notifications for the bell/inbox channel.
type InboxStore interface {
	Add(ctx context.Context, userID int64, n core.Notification) error
	List(ctx context.Context, userID int64, limit int) ([]Item, error)
	UnreadCount(ctx context.Context, userID int64) (int, error)
	MarkAllRead(ctx context.Context, userID int64) error
}

// CurrentFunc resolves the logged-in user (host middleware).
type CurrentFunc func(*gin.Context) (*core.User, bool)

type inboxHandler struct {
	store   InboxStore
	current CurrentFunc
	tmpl    *template.Template
}

// InboxViews returns the /p/inbox site page (list + mark-all-read). The host
// reads InboxStore.UnreadCount separately for its navbar bell.
func InboxViews(store InboxStore, current CurrentFunc) ([]core.View, error) {
	t, err := template.ParseFS(viewFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := &inboxHandler{store: store, current: current, tmpl: t}
	return []core.View{{
		Slug: "inbox", Title: "Inbox", Slot: core.SlotSitePage, MinRole: core.RoleUser,
		Render: h.render,
		Actions: map[string]func(*gin.Context) (template.HTML, error){
			"read-all": h.readAll,
		},
	}}, nil
}

func (h *inboxHandler) render(c *gin.Context) (template.HTML, error) {
	u, ok := h.current(c)
	if !ok {
		return "", nil
	}
	items, err := h.store.List(c.Request.Context(), u.ID, 50)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "inbox.html", map[string]any{"Items": items}); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (h *inboxHandler) readAll(c *gin.Context) (template.HTML, error) {
	if u, ok := h.current(c); ok {
		_ = h.store.MarkAllRead(c.Request.Context(), u.ID)
	}
	c.Redirect(http.StatusSeeOther, "/p/inbox")
	return "", nil
}
