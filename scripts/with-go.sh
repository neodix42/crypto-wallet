#!/bin/sh
# Go-only commands must not download or depend on Node/Electron/test tools.
set -eu
cd "$(dirname "$0")/.."
fail() { printf 'Build prerequisite: %s\n' "$*" >&2; exit 1; }
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail 'This target is Linux amd64. Other operating systems/architectures have not been validated.'
command -v go >/dev/null 2>&1 || fail 'Go 1.21+ is required to bootstrap the pinned Go 1.27.3 toolchain. Install Go, then rerun make.'
export GOTOOLCHAIN=go1.27.3
if ! go version >/dev/null; then fail 'Could not load Go 1.27.3. Check network access to proxy.golang.org and the Go toolchain cache.'; fi
exec "$@"
