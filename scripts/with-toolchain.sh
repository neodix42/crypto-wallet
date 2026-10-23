#!/bin/sh
# Use the reviewed build runtimes without relying on an interactive shell setup.
set -eu
cd "$(dirname "$0")/.."
fail() { printf 'Build prerequisite: %s\n' "$*" >&2; exit 1; }
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail 'This package target is Linux amd64. Other operating systems/architectures have not been validated.'
command -v go >/dev/null 2>&1 || fail 'Go 1.21+ is required to bootstrap the pinned Go 1.27.3 toolchain. Install Go, then rerun make.'
export GOTOOLCHAIN=go1.27.3
if ! go version >/dev/null; then fail 'Could not load Go 1.27.3. Check network access to proxy.golang.org and the Go toolchain cache.'; fi
node_pin=24.19.0
npm_pin=11.17.0
if ! command -v node >/dev/null 2>&1 || [ "$(node --version 2>/dev/null || true)" != "v$node_pin" ] || ! command -v npm >/dev/null 2>&1 || [ "$(npm --version 2>/dev/null || true)" != "$npm_pin" ]; then
  command -v python3 >/dev/null 2>&1 || fail 'python3 is required to bootstrap the pinned Node runtime.'
  node_dir="$PWD/.tools/node-v$node_pin-linux-x64"
  if [ ! -x "$node_dir/bin/node" ]; then
    printf 'Installing pinned Node %s and npm %s into .tools/ (no system changes).\n' "$node_pin" "$npm_pin"
    python3 scripts/bootstrap-node.py
  fi
  PATH="$node_dir/bin:$PATH"
  export PATH
fi
[ "$(node --version)" = "v$node_pin" ] || fail "Expected Node $node_pin after bootstrap. Remove an incomplete .tools/node-v$node_pin-linux-x64 directory and retry."
[ "$(npm --version)" = "$npm_pin" ] || fail "Expected npm $npm_pin in the pinned Node distribution."
exec "$@"
