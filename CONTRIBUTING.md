# Contributing to loon-baseline

This module supplies the parts of a site that every host needs and no host
should write twice: authentication, sessions, password hashing, TOTP, the
API-key store, rate limiting, the maintenance switch, and the self-service
account, inbox and admin-users pages.

Which means a mistake here is a mistake in everybody's login. Most of what
follows is about that.

## Running it

```sh
make help      # the targets
make check     # what CI runs: fmt, vet, sqllint, test
make itest     # the tests that need a real Postgres and Redis
```

The Go toolchain runs **in a container**, via `scripts/go.sh`. On Windows an
anti-virus quarantines freshly built unsigned binaries, and the symptom is not
an obvious error — it is a toolchain reporting `no such tool "compile"` because
the compiler disappeared between two commands.

**`scripts/go.sh` mounts the PARENT directory**, not this repo. `go.mod` carries
`replace github.com/the-loon-clan/loon => ../loon`, so a container that could
only see this repo would fail to resolve the module graph before compiling a
line. Keep `loon` checked out beside this one.

`make check GO=go` uses the toolchain on your machine instead. CI runs exactly
that — a clean Linux container has nothing to work around — so if `make check`
passes locally it passes in CI, and if it does not, that is a bug in the
Makefile rather than something to work around.

## Never point the integration tests at a real service

`make itest` starts a throwaway Postgres on 5597 and a Redis on 6397 and
removes them afterwards. Use it.

The Redis test calls **`FlushDB`** — it wipes the entire database. It reads
`REDIS_TEST_ADDR`, deliberately, and **not** `REDIS_ADDR`: the latter is the
*operator's* switch, set in every compose file in this project, so a test gated
on it would have flushed whatever Redis the app was using the moment somebody
ran `go test ./...` with the app's environment loaded. It was gated on
`REDIS_ADDR` until 21 August 2026.

The same rule holds for `LOON_TEST_DSN`: the Postgres tests create and drop
schemas.

If you add a test that needs a service, gate it on a `*_TEST_*` variable and
add it to `make itest`. CI asserts that none of them **skip** — a skipped test
and a passing one are the same green tick, and these are the ones that exercise
real SCAN+DEL batching and real schema round-trips.

## What a change costs

Three repositories build on this one, and it sits *below* `loon-plugins` in the
graph: it may depend on `loon`, and it must **not** depend on the plugins
module. That is not style. When the CSRF token seam lived in
`loon-plugins/pluginapi`, there was no reachable way for anything here to get a
token, and eight POST forms in this repo — change password, regenerate API key,
the admin set-role and reset-password forms — answered 403 to everyone who
clicked them, for as long as the host had CSRF middleware. The seam is in
`loon/core` now.

So: if something here needs a seam that lives in the plugins module, the seam
is in the wrong place. Move it down to `loon`; do not add the dependency.

For anything exported, say in the PR what a consumer has to do about it. A
renamed field or a method added to an interface compiles fine here and nowhere
else.

## Style

**Comments say why, not what.** The code says what it does. What it cannot say
is the alternative that was tried, the bug a line prevents, or why an
obvious-looking simplification is wrong.

**SQL is constant-only.** `scripts/lint-sql` fails the build on a statement
assembled by concatenation or formatting, because that is how parameterisation
is actually lost.

**Render a form's CSRF field unconditionally**, empty token or not. A host with
no CSRF middleware is legitimate and an empty hidden field is ignored; a
*missing* field is a 403 the person clicking cannot diagnose. `core.CSRFFieldName`
and `core.CSRFFromRequest` are the two pieces.

## Security

Do not open a public issue for a vulnerability. [SECURITY.md](SECURITY.md) has
the process.
