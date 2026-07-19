// Package authflow is the reusable account lifecycle over loon-baseline's
// pieces: register, authenticate (username OR email, with pepper-rotation
// rehash), and change-password — the logic every host duplicates, factored out
// so a host (demo, and eventually prod) keeps only its templates. It composes
// users.Store + password.Hasher + the session cookie; it renders nothing.
package authflow

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"

	"github.com/the-loon-clan/loon-baseline/password"
	"github.com/the-loon-clan/loon-baseline/session"
	"github.com/the-loon-clan/loon-baseline/users"
)

var (
	ErrUsernameTaken  = errors.New("username is already taken")
	ErrBadCredentials = errors.New("invalid username or password")
	ErrWeakPassword   = errors.New("password is too short")
	ErrEmptyUsername  = errors.New("username is required")
)

// Flow bundles the collaborators. DefaultRole/MinPasswordLen have sane
// zero-value fallbacks (RoleUser, 8).
type Flow struct {
	Users          users.Store
	Hasher         password.Hasher
	DefaultRole    core.Role
	MinPasswordLen int
}

func (f Flow) minLen() int {
	if f.MinPasswordLen <= 0 {
		return 8
	}
	return f.MinPasswordLen
}

// Register validates + creates a user with a hashed password.
func (f Flow) Register(ctx context.Context, username, email, plaintext string) (*users.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, ErrEmptyUsername
	}
	if len(plaintext) < f.minLen() {
		return nil, ErrWeakPassword
	}
	if _, err := f.Users.ByUsername(ctx, username); err == nil {
		return nil, ErrUsernameTaken
	} else if !errors.Is(err, users.ErrNotFound) {
		return nil, err
	}
	hash, err := f.Hasher.Hash(plaintext)
	if err != nil {
		return nil, err
	}
	u := &users.User{Username: username, Email: strings.TrimSpace(email), PasswordHash: hash, Role: f.DefaultRole}
	id, err := f.Users.Create(ctx, u)
	if err != nil {
		return nil, err
	}
	u.ID = id
	return u, nil
}

// Authenticate verifies an identifier (username or email) + password, and
// transparently rehashes under the current pepper when needed. Returns
// ErrBadCredentials on any mismatch (no user-enumeration signal).
func (f Flow) Authenticate(ctx context.Context, identifier, plaintext string) (*users.User, error) {
	identifier = strings.TrimSpace(identifier)
	u, err := f.Users.ByUsername(ctx, identifier)
	if errors.Is(err, users.ErrNotFound) {
		u, err = f.Users.ByEmail(ctx, identifier)
	}
	if errors.Is(err, users.ErrNotFound) {
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, needsRehash := f.Hasher.Verify(u.PasswordHash, plaintext)
	if !ok {
		return nil, ErrBadCredentials
	}
	if needsRehash {
		if h, herr := f.Hasher.Hash(plaintext); herr == nil {
			_ = f.Users.UpdatePasswordHash(ctx, u.ID, h)
		}
	}
	return u, nil
}

// ChangePassword verifies the current password then sets a new one.
func (f Flow) ChangePassword(ctx context.Context, id int64, current, next string) error {
	u, err := f.Users.ByID(ctx, id)
	if err != nil {
		return err
	}
	if ok, _ := f.Hasher.Verify(u.PasswordHash, current); !ok {
		return ErrBadCredentials
	}
	if len(next) < f.minLen() {
		return ErrWeakPassword
	}
	h, err := f.Hasher.Hash(next)
	if err != nil {
		return err
	}
	return f.Users.UpdatePasswordHash(ctx, id, h)
}

// Issue stamps a logged-in session cookie for u. A richer host passes a hashed
// IP + password_changed_at; the base flow passes zero (no IP-pin, no
// password-change epoch), matching the demo.
func (f Flow) Issue(c *gin.Context, u *users.User) error {
	return session.Issue(c, u.ID, "", 0)
}
