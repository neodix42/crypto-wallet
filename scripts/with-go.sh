#!/bin/sh
# Go commands do not bootstrap Node/Electron; Wails uses the reviewed Linux ABI.
set -eu
cd "$(dirname "$0")/.."
fail() { printf 'Build prerequisite: %s\n' "$*" >&2; exit 1; }
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail 'This target is Linux amd64. Other operating systems/architectures have not been validated.'
command -v go >/dev/null 2>&1 || fail 'Go 1.21+ is required to bootstrap the pinned Go 1.27.3 toolchain. Install Go, then rerun make.'
export GOTOOLCHAIN=go1.27.3
if ! go version >/dev/null; then fail 'Could not load Go 1.27.3. Check network access to proxy.golang.org and the Go toolchain cache.'; fi
# A rootless SDK is optional; installed development packages work without it.
if command -v cc >/dev/null 2>&1; then . ./scripts/native-env.sh; fi
# Wails defaults to the older WebKitGTK4.0 ABI. Use4.1 consistently for builds,
# vet and tests without enabling production or portable implicitly. Preserve
# caller tags, including portable/memory-test instrumentation when requested.
command -v python3 >/dev/null 2>&1 || fail 'python3 is required to configure the pinned Linux WebKitGTK build ABI.'
GOFLAGS=$(python3 - <<'PY'
import os, shlex
flags=shlex.split(os.environ.get('GOFLAGS', ''))
tags=[]; other=[]
for flag in flags:
    if flag.startswith('-tags='):
        tags.extend(flag[6:].replace(',', ' ').split())
    else:
        other.append(flag)
print(shlex.join(other + ['-tags=' + ','.join(dict.fromkeys(tags + ['webkit2_41']))]))
PY
)
export GOFLAGS
exec "$@"
