package authtoken

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/the-loon-clan/loon/core"

	"github.com/the-loon-clan/loon-baseline/password"
	"github.com/the-loon-clan/loon-baseline/users"
)

// memTokens is an in-memory Store for the flow tests.
type memTokens struct {
	rows []struct {
		userID  int64
		purpose Purpose
		hash    string
		expires time.Time
		used    bool
	}
}

func (m *memTokens) Create(_ context.Context, userID int64, p Purpose, hash string, exp time.Time) error {
	m.rows = append(m.rows, struct {
		userID  int64
		purpose Purpose
		hash    string
		expires time.Time
		used    bool
	}{userID, p, hash, exp, false})
	return nil
}

func (m *memTokens) Consume(_ context.Context, hash string, p Purpose) (int64, error) {
	for i := range m.rows {
		r := &m.rows[i]
		if r.hash == hash && r.purpose == p && !r.used && time.Now().Before(r.expires) {
			r.used = true
			return r.userID, nil
		}
	}
	return 0, ErrInvalidToken
}

// memUsers is a minimal users.Store for the flow tests.
type memUsers struct {
	byEmail  map[string]*users.User
	verified map[int64]bool
	pw       map[int64]string
}

func newMemUsers() *memUsers {
	return &memUsers{byEmail: map[string]*users.User{}, verified: map[int64]bool{}, pw: map[int64]string{}}
}
func (m *memUsers) Create(context.Context, *users.User) (int64, error) { return 0, nil }
func (m *memUsers) ByID(context.Context, int64) (*users.User, error)   { return nil, users.ErrNotFound }
func (m *memUsers) ByUsername(context.Context, string) (*users.User, error) {
	return nil, users.ErrNotFound
}
func (m *memUsers) ByEmail(_ context.Context, e string) (*users.User, error) {
	if u, ok := m.byEmail[e]; ok {
		return u, nil
	}
	return nil, users.ErrNotFound
}
func (m *memUsers) IDByName(context.Context, string) (int64, error) { return 0, users.ErrNotFound }
func (m *memUsers) UpdatePasswordHash(_ context.Context, id int64, h string) error {
	m.pw[id] = h
	return nil
}
func (m *memUsers) SetRole(context.Context, int64, core.Role) error { return nil }
func (m *memUsers) SetEmailVerified(_ context.Context, id int64, v bool) error {
	m.verified[id] = v
	return nil
}
func (m *memUsers) List(context.Context, int, int) ([]*users.User, int, error) { return nil, 0, nil }

func newFlow() (Flow, *memUsers, *[]string) {
	mu := newMemUsers()
	mu.byEmail["a@x.com"] = &users.User{ID: 1, Email: "a@x.com"}
	var sent []string
	f := Flow{
		Tokens: &memTokens{}, Users: mu, Hasher: password.Hasher{}, BaseURL: "http://h",
		Mail: func(to, subject, body string) error { sent = append(sent, body); return nil },
	}
	return f, mu, &sent
}

// linkToken extracts the ?token= value from the last mailed link.
func linkToken(body string) string {
	idx := strings.Index(body, "token=")
	if idx < 0 {
		return ""
	}
	rest := body[idx+len("token="):]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return rest
}

func TestResetFlow(t *testing.T) {
	ctx := context.Background()
	f, mu, sent := newFlow()

	// unknown email: silent success, no mail
	if err := f.RequestReset(ctx, "nobody@x.com"); err != nil {
		t.Fatalf("unknown email: %v", err)
	}
	if len(*sent) != 0 {
		t.Fatal("no mail should be sent for unknown email")
	}

	// known email: a reset link is mailed
	if err := f.RequestReset(ctx, "a@x.com"); err != nil {
		t.Fatal(err)
	}
	tok := linkToken((*sent)[len(*sent)-1])
	if tok == "" {
		t.Fatal("no token in reset link")
	}

	// weak password rejected before consuming the token
	if err := f.PerformReset(ctx, tok, "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak pw: %v", err)
	}
	// valid reset sets the hash
	if err := f.PerformReset(ctx, tok, "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if mu.pw[1] == "" {
		t.Fatal("password hash not updated")
	}
	// token is single-use: second redemption fails
	if err := f.PerformReset(ctx, tok, "another12345"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reused token: %v", err)
	}
	// garbage token fails
	if err := f.PerformReset(ctx, "garbage", "another12345"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("garbage token: %v", err)
	}
}

func TestVerifyFlow(t *testing.T) {
	ctx := context.Background()
	f, mu, sent := newFlow()

	if err := f.SendVerify(ctx, 1, "a@x.com"); err != nil {
		t.Fatal(err)
	}
	tok := linkToken((*sent)[len(*sent)-1])

	// a reset token can't be redeemed as a verify token (purpose scoping):
	// mint a reset token and try to confirm-verify it.
	_ = f.RequestReset(ctx, "a@x.com")
	resetTok := linkToken((*sent)[len(*sent)-1])
	if _, err := f.ConfirmVerify(ctx, resetTok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reset token accepted as verify: %v", err)
	}

	// the real verify token flips the flag
	if _, err := f.ConfirmVerify(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if !mu.verified[1] {
		t.Fatal("email not marked verified")
	}
}
