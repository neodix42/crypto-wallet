#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
trap 'code=$?; if [ "$code" -ne 0 ]; then printf "Build failed (exit %s). See the command/error above; prerequisites are documented in README.md.\n" "$code" >&2; fi' EXIT
command -v cc >/dev/null 2>&1 || { printf 'Install build-essential: CGO requires a C compiler.\n' >&2; exit 1; }
version=$(python3 scripts/version.py)
printf 'Building LocalWallet %s with %s, Node %s, npm %s\n' "$version" "$(go version)" "$(node --version)" "$(npm --version)"
mkdir -p bin
for component in core signer launcher; do
  printf 'Compiling wallet-%s...\n' "$component"
  go build -trimpath -ldflags="-s -w -buildid= -X localwallet/internal/buildinfo.Version=$version" -o "bin/wallet-$component" "./cmd/wallet-$component"
done
python3 - <<'PY'
import hashlib,json,pathlib
root=pathlib.Path('bin')
(root/'manifest.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in root.glob('wallet-*')},sort_keys=True)+'\n')
PY
npm --prefix desktop ci --ignore-scripts
npm --prefix desktop run build
node --check desktop/ui/renderer.js
printf 'Build complete: bin/wallet-core, bin/wallet-signer, bin/wallet-launcher.\nUse make install for the complete runnable Electron wallet at ./wallet.\n'
