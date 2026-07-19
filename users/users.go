// Package users is loon-baseline's host user store: the base identity every
// site needs, which loon/core deliberately leaves to the host. It's a Store
// INTERFACE (so a site with its own users table — like the prod indexer, whose
// users table is migration 1 and FK'd everywhere — just implements it) plus a
// Postgres REFERENCE implementation (so a new host gets a working users table
// for free). The auth pages + admin user-management in loon-baseline operate
// over the interface, never a concrete table.
package users

import (
	"context"
	"errors"
	"time"

	"github.com/the-loon-clan/loon/core"
)

// ErrNotFound is returned by the by-key lookups when no user matches.
var ErrNotFound = errors.New("users: not found")

// User is the full host-side user record (unlike core.User, which is the
// trimmed plugin-facing projection). It carries the password hash + email the
// auth flows need; project to core.User at the seam via ToCore.
type User struct {
	ID            int64
	Username      string
	Email         string
	PasswordHash  string
	Role          core.Role
	EmailVerified bool
	CreatedAt     time.Time
}

// ToCore projects to the plugin-facing core.User (drops the password hash).
func (u *User) ToCore() *core.User {
	if u == nil {
		return nil
	}
	return &core.User{ID: u.ID, Username: u.Username, Email: u.Email, Role: u.Role, CreatedAt: u.CreatedAt}
}

// Store is the host user store. by-key lookups return ErrNotFound when absent
// (callers use errors.Is). A host with its own schema implements this; new
// hosts use the Postgres reference impl (pgstore.go).
type Store interface {
	Create(ctx context.Context, u *User) (int64, error)
	ByID(ctx context.Context, id int64) (*User, error)
	ByUsername(ctx context.Context, username string) (*User, error)
	ByEmail(ctx context.Context, email string) (*User, error)
	// IDByName resolves a username to just its id (ErrNotFound if none) — a
	// lightweight lookup for callers that need only the id, e.g. attributing a
	// failed login attempt to the targeted account. Matches loginlog.Resolver.
	IDByName(ctx context.Context, username string) (int64, error)
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	SetRole(ctx context.Context, id int64, role core.Role) error
	// SetEmailVerified marks (or clears) a user's email-verified flag.
	SetEmailVerified(ctx context.Context, id int64, verified bool) error
	List(ctx context.Context, offset, limit int) (users []*User, total int, err error)
}
