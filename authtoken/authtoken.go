// Package authtoken is loon-baseline's single-use-token engine behind two host
// flows loon/core leaves to the host: PASSWORD RESET and EMAIL VERIFICATION.
//
// A token is a random secret handed to the user in an emailed link; only its
// SHA-256 hash is ever stored, so a database leak can't be replayed. Tokens are
// single-use (marked used atomically on redemption) and time-limited.
//
// The package owns the token store + the flow logic; the HOST supplies the
// mailer (Mail) and base URL and owns the routes + templates — same division as
// authflow. Nothing here renders HTML or talks SMTP directly.
package authtoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/the-loon-clan/loon-baseline/password"
	"github.com/the-loon-clan/loon-baseline/users"
)

// Purpose scopes a token so a reset token can't be redeemed as a verify token.
type Purpose string

const (
	PasswordReset Purpose = "password_reset"
	EmailVerify   Purpose = "email_verify"
)

// Store persists single-use tokens (by hash).
type Store interface {
	// Create records a token hash for a user + purpose with an expiry.
	Create(ctx context.Context, userID int64, purpose Purpose, tokenHash string, expiresAt time.Time) error
	// Consume atomically marks a matching, unexpired, unused token used and
	// returns its user id; ErrInvalidToken if none matches.
	Consume(ctx context.Context, tokenHash string, purpose Purpose) (int64, error)
}

var (
	// ErrInvalidToken covers not-found / expired / already-used — one opaque
	// error so a caller can't distinguish them.
	ErrInvalidToken = errors.New("authtoken: invalid or expired token")
	ErrWeakPassword = errors.New("authtoken: password too short")
)

func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// issue mints a random token, stores its hash, and returns the plaintext (which
// only ever lives in the emailed link).
func issue(ctx context.Context, store Store, userID int64, purpose Purpose, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	plaintext := base64.RawURLEncoding.EncodeToString(buf)
	if err := store.Create(ctx, userID, purpose, hashToken(plaintext), time.Now().Add(ttl)); err != nil {
		return "", err
	}
	return plaintext, nil
}

// redeem hashes the presented token and consumes it, returning the user id.
func redeem(ctx context.Context, store Store, plaintext string, purpose Purpose) (int64, error) {
	if strings.TrimSpace(plaintext) == "" {
		return 0, ErrInvalidToken
	}
	return store.Consume(ctx, hashToken(plaintext), purpose)
}

// Flow bundles the reset + verify operations over the token store, the user
// store, and a host-supplied mailer. BaseURL is the site origin used to build
// links (no trailing slash). ResetTTL/VerifyTTL default to 1h/24h if zero.
type Flow struct {
	Tokens    Store
	Users     users.Store
	Hasher    password.Hasher
	Mail      func(to, subject, body string) error
	BaseURL   string
	ResetTTL  time.Duration
	VerifyTTL time.Duration
	MinPwLen  int // minimum new-password length (default 8)
}

func (f Flow) resetTTL() time.Duration {
	if f.ResetTTL > 0 {
		return f.ResetTTL
	}
	return time.Hour
}

func (f Flow) verifyTTL() time.Duration {
	if f.VerifyTTL > 0 {
		return f.VerifyTTL
	}
	return 24 * time.Hour
}

func (f Flow) minLen() int {
	if f.MinPwLen > 0 {
		return f.MinPwLen
	}
	return 8
}

// RequestReset emails a password-reset link for the account with this email.
// It is intentionally SILENT about whether the email exists (no account
// enumeration): callers should always show the same "check your inbox" message.
func (f Flow) RequestReset(ctx context.Context, email string) error {
	u, err := f.Users.ByEmail(ctx, email)
	if errors.Is(err, users.ErrNotFound) {
		return nil // pretend success
	}
	if err != nil {
		return err
	}
	token, err := issue(ctx, f.Tokens, u.ID, PasswordReset, f.resetTTL())
	if err != nil {
		return err
	}
	link := fmt.Sprintf("%s/reset?token=%s", f.BaseURL, token)
	return f.Mail(u.Email, "Reset your password",
		"Someone requested a password reset for your account.\n\nReset it here (valid for one hour):\n"+link+
			"\n\nIf this wasn't you, ignore this email.")
}

// PerformReset validates the token and sets a new password.
func (f Flow) PerformReset(ctx context.Context, token, newPassword string) error {
	if len(newPassword) < f.minLen() {
		return ErrWeakPassword
	}
	userID, err := redeem(ctx, f.Tokens, token, PasswordReset)
	if err != nil {
		return err
	}
	hash, err := f.Hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	return f.Users.UpdatePasswordHash(ctx, userID, hash)
}

// SendVerify emails an email-verification link to the given address for userID
// (call after registration, or on a "resend" action).
func (f Flow) SendVerify(ctx context.Context, userID int64, email string) error {
	if email == "" {
		return nil // nothing to verify
	}
	token, err := issue(ctx, f.Tokens, userID, EmailVerify, f.verifyTTL())
	if err != nil {
		return err
	}
	link := fmt.Sprintf("%s/verify?token=%s", f.BaseURL, token)
	return f.Mail(email, "Verify your email",
		"Confirm your email address by visiting:\n"+link+"\n\nThe link is valid for 24 hours.")
}

// ConfirmVerify validates a verification token and marks the user's email
// verified, returning the user id.
func (f Flow) ConfirmVerify(ctx context.Context, token string) (int64, error) {
	userID, err := redeem(ctx, f.Tokens, token, EmailVerify)
	if err != nil {
		return 0, err
	}
	if err := f.Users.SetEmailVerified(ctx, userID, true); err != nil {
		return 0, err
	}
	return userID, nil
}
