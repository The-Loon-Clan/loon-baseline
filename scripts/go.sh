#!/usr/bin/env bash
# Run the Go toolchain in a container, never on the host.
#
#   scripts/go.sh test ./...
#   scripts/go.sh vet ./...
#   scripts/go.sh gofmt -l .        <- the one non-`go` command, see below
#
# Why
# ---
# `go build` and `go test` write executables into the working tree and into a
# cache, and a Windows anti-virus treats freshly produced unsigned binaries as
# exactly what they look like. The symptoms are not obvious errors — they are a
# toolchain that reports `no such tool "compile"` because the compiler was
# quarantined between two commands.
#
# In a container nothing lands on the host filesystem except the source that was
# already there. It also pins the Go version to the one go.mod asks for rather
# than whatever the host happens to have, which is why CI does the same.
#
# THE PARENT DIRECTORY IS THE MOUNT, not this repo. go.mod carries
# `replace github.com/the-loon-clan/loon => ../loon`, so a container that could
# only see loon-baseline would fail to resolve the module graph before it
# compiled a line. Same as loon-plugins' copy; loon's own mounts itself, having
# no sibling to reach.
set -euo pipefail

IMAGE="${GO_IMAGE:-golang:1.26}"
CACHE_VOL="loon-gomod"
BUILD_VOL="loon-gobuild"

# Shared with the sibling repos on purpose: the module cache is keyed by module
# and version, so repos that depend on the same things download them once.
docker volume create "$CACHE_VOL" >/dev/null
docker volume create "$BUILD_VOL" >/dev/null

# MSYS_NO_PATHCONV: git-bash on Windows rewrites /src into a Windows path before
# docker sees it, and the mount silently lands somewhere useless.
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PARENT_DIR="$(dirname "$REPO_DIR")"
REPO_NAME="$(basename "$REPO_DIR")"

# gofmt is not a `go` subcommand, and formatting has to be checkable in the same
# container as everything else — `go fmt` WRITES, which is not what a check
# wants. So this one word is passed through as the binary rather than as an
# argument to `go`.
BIN=go
if [ "${1:-}" = "gofmt" ]; then
  BIN=gofmt
  shift
fi

MSYS_NO_PATHCONV=1 exec docker run --rm \
  -v "$PARENT_DIR":/src \
  -v "$CACHE_VOL":/go/pkg/mod \
  -v "$BUILD_VOL":/root/.cache/go-build \
  -w "/src/$REPO_NAME" \
  -e GOWORK=off \
  -e GOFLAGS="-buildvcs=false -mod=mod" \
  `# Forwarded, not inherited: a container gets none of the host's environment` \
  `# unless it is named here, so LOON_TEST_DSN=... scripts/go.sh test would set` \
  `# the variable in the SHELL and not in the process that reads it — and any` \
  `# test gated on it would skip while looking like it ran.` \
  -e LOON_TEST_DSN \
  -e REDIS_TEST_ADDR \
  -e CI \
  `# --network host so a container can reach a throwaway Postgres or Redis` \
  `# published on the host.` \
  --network host \
  "$IMAGE" "$BIN" "$@"
