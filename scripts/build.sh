#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# A separate entry point keeps every make target on the same pinned runtimes.
if [ "${1:-}" = --components ]; then
  exec ./scripts/with-go.sh ./scripts/build-pinned.sh "$@"
fi
exec ./scripts/with-toolchain.sh ./scripts/build-pinned.sh "$@"
