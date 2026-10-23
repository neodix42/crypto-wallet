#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test "$(go version | cut -d' ' -f3)" = go1.27.3
test "$(node --version)" = v24.19.0
mkdir -p bin
go build -trimpath -ldflags='-s -w -buildid=' -o bin/wallet-core ./cmd/wallet-core
go build -trimpath -ldflags='-s -w -buildid=' -o bin/wallet-signer ./cmd/wallet-signer
go build -trimpath -ldflags='-s -w -buildid=' -o bin/wallet-launcher ./cmd/wallet-launcher
python3 - <<'PY'
import hashlib,json,pathlib
root=pathlib.Path('bin')
(root/'manifest.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in root.glob('wallet-*')},sort_keys=True)+'\n')
PY
npm --prefix desktop ci --ignore-scripts
npm --prefix desktop run build

node --check desktop/ui/renderer.js
