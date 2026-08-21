# Security

## Reporting a vulnerability

Please report privately through GitHub's
[security advisories](https://github.com/The-Loon-Clan/loon-baseline/security/advisories/new)
rather than opening a public issue.

Include what you did, what happened, and what you expected. A proof of concept
helps but is not required — a clear description of the flaw is enough to act on.

This is a small project without a paid security team, so expect a first reply
within a week rather than within hours.

## What this software is

**The parts of a site every host needs and no host should write twice**:
password hashing, sessions, the login flow, TOTP, single-use email/reset
tokens, the API-key store, rate limiting, the maintenance switch, and the
self-service account, inbox and admin-users pages.

`loon` is the framework and defines the seams; this fills them. Which means a
flaw here is a flaw in *everybody's* login, and it is the most security-bearing
of the four repositories despite being the least visible.

## What is defended, and how

**Passwords.** bcrypt, over an optional HMAC-SHA256 pepper applied *before*
hashing. The pepper is what keeps the bcrypt input inside its 72-byte limit
whatever the password's length — a naive implementation silently truncates at
72 bytes, so a long passphrase and its first 72 bytes become the same
credential. `needsRehash` lets a host raise the cost without a migration.

**Sessions.** `HttpOnly` always, `SameSite=Lax` always, `Secure` when the host
sets it — off by default so a checkout runs over plain HTTP, which any TLS
deployment must change. An epoch stored with the session lets a host invalidate
every session for a user at once, which is what a password change needs.

**Single-use tokens** (email confirmation, password reset). Only the SHA-256 of
the token is stored, so a database read does not yield a usable token. Consuming
one is a single statement:

```sql
UPDATE auth_tokens SET used_at = now()
 WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > now()
RETURNING user_id
```

Every condition is in the `WHERE` and the row is claimed by the same statement
that checks it, so there is no window between "is this token still valid" and
"use it" — two simultaneous redemptions cannot both succeed.

**SQL injection.** Statements must be constants. `scripts/lint-sql` fails the
build on SQL assembled by concatenation or formatting, which is how
parameterisation is actually lost.

**CSRF.** Every POST form this module renders carries a hidden token, read from
the request via `core.CSRFFromRequest`. The field is rendered *unconditionally*,
empty or not: a host with no CSRF middleware is legitimate and an empty field is
ignored, while a **missing** field is a 403 the person clicking cannot diagnose.

That asymmetry is not theoretical. Until 21 August 2026 **eight forms here had
no token at all** — change password, regenerate API key, mark inbox read, the
admin set-role and reset-password forms, and maintenance begin — and every one
of them answered 403 to whoever clicked it, for as long as the host had CSRF
middleware. They were broken rather than exploitable, and nothing reported it:
the host's static audit does not scan this repo, and the live audit that would
have caught them had a one-byte defect that stopped it matching any form.

**Rate limiting.** Memory and Redis backends, for the login path a host must
throttle.

## Known limitations

Stated because a security document that lists only strengths is not useful.

- **API keys are stored in plaintext.** `api_keys.api_key` holds the key, and
  `Resolve` is a direct `SELECT` on it. That is a deliberate trade — a Newznab
  key must be displayable so a member can paste it into a client, and it is
  looked up on every API request — but it means a database read discloses every
  key, and it is not the same protection passwords get.
- **`Secure` is off by default** so a checkout runs over plain HTTP. Any
  deployment behind TLS must turn it on.
- **A destructive test was gated on the operator's own variable.** The Redis
  `DeletePrefix` test calls `FlushDB`, and until 21 August 2026 it read
  `REDIS_ADDR` — which every compose file in this project sets. `go test ./...`
  on a machine with the app's environment loaded would have wiped the running
  site's cache, sessions and rate-limit counters. It reads `REDIS_TEST_ADDR`
  now, and CI asserts the integration tests do not silently skip.
- **The tests did not run in CI until 21 August 2026.** The only workflow ran
  the SQL lint, so 20 test files across 23 packages never executed on a push —
  in the module that holds authentication.
- **No signed releases and no tagged versions.** Consumers pin by commit or by
  a sibling `replace`. Worth having; not there yet.
