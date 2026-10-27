#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
# A separate entry point keeps every make target on the same pinned runtimes.
exec ./scripts/with-go.sh ./scripts/build-pinned.sh "$@"
