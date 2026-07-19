<p align="center">
  <img src="img/logo.png" alt="loon" width="180">
</p>

<h1 align="center">loon-baseline</h1>

<p align="center">The reusable host baseline for sites built on the <a href="https://github.com/The-Loon-Clan/loon">loon</a> plugin framework.</p>

---

loon is a plugin framework, not a runnable site. It deliberately has **no login,
session, or password seam** — auth is security-sensitive and host-shaped, so
`loon/core` exposes only an `AuthService` *adapter* and leaves the implementation
to the host. That leaves every real host re-deriving the same plumbing.

`loon-baseline` is that plumbing, factored out so a demo and a production site
share one battle-tested implementation instead of copy-pasting it. It is a
**library you import**, not a service that owns your users table — your host
keeps its own `users` schema and its product-specific flows (MFA, passkeys,
points, …).

## Packages

| Package | Depends on | What it gives you |
|---|---|---|
| [`session`](session/) | gin | Stateless HMAC-signed session cookies carrying `user id · issued-at · epoch`. Server-side expiry via MaxAge; bump the epoch to invalidate every outstanding session after a password change. |
| [`password`](password/) | x/crypto | bcrypt over an optional HMAC **pepper**, with transparent **pepper rotation** (`Verify` reports `needsRehash`). The exact scheme the prod site uses, so existing hashes stay valid. |
| [`webauth`](webauth/) | gin, loon/core, `session` | The current-user middleware (`Soft` / `Require` / `RequireExact` / `Current`) behind a host `Resolver`, plus `CoreAuth()` which wires it into loon's `core.AuthService`. |

## Usage sketch

```go
sess := session.Manager{Secret: secret}                 // 32+ byte key
pw   := password.Hasher{Pepper: pepper}                 // bcrypt + pepper

auth := webauth.Auth{
    Session: sess,
    Resolve: func(ctx context.Context, id int64) (*core.User, int64, bool) {
        u, ok := lookupUser(id)        // your users table
        return u, u.PasswordEpoch, ok  // epoch 0 disables password-change invalidation
    },
}

// login handler
if ok, needsRehash := pw.Verify(storedHash, submitted); ok {
    if needsRehash { _ = saveHash(userID, mustHash(pw, submitted)) }
    sess.Issue(c, userID, epoch)
}

// gate routes + hand loon the seam
admin := engine.Group("/admin", auth.Require(core.RoleAdmin)...)
rt, _ := core.Boot(ctx, core.Deps{Auth: auth.CoreAuth(), /* … */})
```

The [loon demo site](https://github.com/The-Loon-Clan/loon-demo-site) is the reference
consumer.
