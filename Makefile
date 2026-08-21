# loon-baseline
#
# Everything runs the Go toolchain in a container (scripts/go.sh explains why).
# `make help` lists the targets.
#
# GO=go runs the host toolchain instead, which is what CI does: a clean Linux
# container has no anti-virus to work around, and nesting one container inside
# another buys nothing. The checks themselves are identical either way.

GO ?= bash scripts/go.sh

# gofmt is a separate BINARY, not a go subcommand, so it cannot go through $(GO)
# unchanged. scripts/go.sh takes `gofmt` as its first word and runs the binary;
# the bare toolchain needs the binary named directly, because `go gofmt` is not
# a command — it fails to stderr and prints nothing to stdout, so a target that
# captured the output would read the emptiness as "nothing to format" and
# report CLEAN. A check that cannot run and says it passed is worse than none.
GOFMT ?= $(if $(filter go,$(GO)),gofmt,$(GO) gofmt)

# The throwaway services `itest` starts. Ports chosen so they cannot be confused
# with a development instance, and so all three repos can run at once:
# loon-demo-site uses 5599/6398, loon-plugins 5598, this one 5597/6397.
ITEST_DB_PORT ?= 5597
ITEST_REDIS_PORT ?= 6397
ITEST_DB_NAME ?= loon_baseline_test

.PHONY: help test itest vet fmt sqllint tidy check

## help: list the targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'

## test: the unit suite. The tests that need services SKIP without their env
## vars set — see itest.
test:
	$(GO) test ./...

## itest: the tests that need a real Postgres and Redis, against disposable ones
##
## NEVER a development instance. The Redis test calls FlushDB — it wipes the
## whole database — which is why it reads REDIS_TEST_ADDR and not REDIS_ADDR.
## REDIS_ADDR is the operator's own switch, set in every compose file in this
## project, and this test was gated on it until 21 Aug 2026: `go test ./...` on
## a machine with the app's environment loaded would have flushed the running
## site's cache, sessions and rate-limit counters.
##
## The status is kept and the cleanup still runs, so a failing test fails the
## target rather than being swallowed by the teardown.
itest:
	@docker rm -fv loon-baseline-itestdb loon-baseline-itestredis >/dev/null 2>&1 || true
	@docker run -d --name loon-baseline-itestdb -e POSTGRES_USER=demo -e POSTGRES_PASSWORD=demo \
	  -e POSTGRES_DB=$(ITEST_DB_NAME) -p $(ITEST_DB_PORT):5432 postgres:16-alpine >/dev/null
	@docker run -d --name loon-baseline-itestredis -p $(ITEST_REDIS_PORT):6379 redis:7-alpine >/dev/null
	@sleep 8
	@set +e; \
	 LOON_TEST_DSN="postgres://demo:demo@localhost:$(ITEST_DB_PORT)/$(ITEST_DB_NAME)?sslmode=disable" \
	 REDIS_TEST_ADDR="localhost:$(ITEST_REDIS_PORT)" \
	 $(GO) test -count=1 ./... ; \
	 status=$$?; \
	 docker rm -fv loon-baseline-itestdb loon-baseline-itestredis >/dev/null 2>&1; \
	 exit $$status

## vet: go vet
vet:
	$(GO) vet ./...

## fmt: report anything gofmt would change (an error, not a suggestion)
fmt:
	@out=$$($(GOFMT) -l .) || { echo "gofmt could not run: $(GOFMT)"; exit 1; }; \
	 if [ -n "$$out" ]; then echo "gofmt would change:"; echo "$$out"; exit 1; fi; \
	 echo "gofmt: clean"

## sqllint: the constant-only-SQL guard (scripts/lint-sql)
sqllint:
	$(GO) run ./scripts/lint-sql ./...

## tidy: go.mod and go.sum must already be what `go mod tidy` produces
##
## A stray requirement or a missing sum is invisible until somebody clones
## fresh — the local module cache papers over both, so it fails for the
## newcomer and nobody else.
tidy:
	@cp go.mod /tmp/go.mod.bak; cp go.sum /tmp/go.sum.bak; \
	 $(GO) mod tidy; \
	 if ! diff -u /tmp/go.mod.bak go.mod || ! diff -u /tmp/go.sum.bak go.sum; then \
	   echo "go.mod/go.sum are not tidy — run 'go mod tidy' and commit the result"; \
	   cp /tmp/go.mod.bak go.mod; cp /tmp/go.sum.bak go.sum; exit 1; \
	 fi; \
	 echo "go.mod: tidy"

## check: everything CI runs except itest
check: fmt vet sqllint test
