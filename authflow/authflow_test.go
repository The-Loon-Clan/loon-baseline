package authflow

import (
	"context"
	"errors"
	"testing"

	"github.com/the-loon-clan/loon/core"

	"github.com/the-loon-clan/loon-baseline/password"
	"github.com/the-loon-clan/loon-baseline/users"
)

// memStore is an in-memory users.Store for testing the flow without a DB.
type memStore struct {
	byID map[int64]*users.User
	next int64
}

func newMem() *memStore { return &memStore{byID: map[int64]*users.User{}} }

func (m *memStore) Create(_ context.Context, u *users.User) (int64, error) {
	m.next++
	u.ID = m.next
	cp := *u
	m.byID[m.next] = &cp
	return m.next, nil
}
func (m *memStore) ByID(_ context.Context, id int64) (*users.User, error) {
	if u, ok := m.byID[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, users.ErrNotFound
}
func (m *memStore) ByUsername(_ context.Context, name string) (*users.User, error) {
	for _, u := range m.byID {
		if u.Username == name {
			cp := *u
			return &cp, nil
		}
	}
	return nil, users.ErrNotFound
}
func (m *memStore) ByEmail(_ context.Context, email string) (*users.User, error) {
	for _, u := range m.byID {
		if email != "" && u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, users.ErrNotFound
}
func (m *memStore) IDByName(_ context.Context, name string) (int64, error) {
	for _, u := range m.byID {
		if u.Username == name {
			return u.ID, nil
		}
	}
	return 0, users.ErrNotFound
}
func (m *memStore) UpdatePasswordHash(_ context.Context, id int64, hash string) error {
	if u, ok := m.byID[id]; ok {
		u.PasswordHash = hash
	}
	return nil
}
func (m *memStore) SetRole(_ context.Context, id int64, role core.Role) error {
	if u, ok := m.byID[id]; ok {
		u.Role = role
	}
	return nil
}
func (m *memStore) SetEmailVerified(_ context.Context, id int64, v bool) error {
	if u, ok := m.byID[id]; ok {
		u.EmailVerified = v
	}
	return nil
}
func (m *memStore) List(_ context.Context, _, _ int) ([]*users.User, int, error) {
	return nil, len(m.byID), nil
}

func newFlow() Flow {
	return Flow{Users: newMem(), Hasher: password.Hasher{}}
}

func TestRegisterAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	f := newFlow()

	// weak password rejected
	if _, err := f.Register(ctx, "carol", "c@x.com", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password err = %v, want ErrWeakPassword", err)
	}
	u, err := f.Register(ctx, "carol", "c@x.com", "hunter2xyz")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.PasswordHash == "" {
		t.Fatalf("registered user = %+v", u)
	}
	// duplicate username rejected
	if _, err := f.Register(ctx, "carol", "", "anotherpw12"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("dup err = %v, want ErrUsernameTaken", err)
	}

	// authenticate by username, then by email
	if got, err := f.Authenticate(ctx, "carol", "hunter2xyz"); err != nil || got.ID != u.ID {
		t.Fatalf("auth by username = %v/%v", got, err)
	}
	if got, err := f.Authenticate(ctx, "c@x.com", "hunter2xyz"); err != nil || got.ID != u.ID {
		t.Fatalf("auth by email = %v/%v", got, err)
	}
	// wrong password + unknown user both ErrBadCredentials (no enumeration)
	if _, err := f.Authenticate(ctx, "carol", "wrong"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("wrong pw err = %v", err)
	}
	if _, err := f.Authenticate(ctx, "nobody", "whatever12"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("unknown user err = %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	f := newFlow()
	u, _ := f.Register(ctx, "dave", "", "original12")
	if err := f.ChangePassword(ctx, u.ID, "wrong", "newpassword1"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("change w/ wrong current = %v", err)
	}
	if err := f.ChangePassword(ctx, u.ID, "original12", "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Authenticate(ctx, "dave", "newpassword1"); err != nil {
		t.Fatalf("auth after change = %v", err)
	}
	if _, err := f.Authenticate(ctx, "dave", "original12"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("old password still works: %v", err)
	}
}
