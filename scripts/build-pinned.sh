#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
trap 'code=$?; if [ "$code" -ne 0 ]; then printf "Build failed (exit %s). See the command/error above; prerequisites are documented in README.md.\n" "$code" >&2; fi' EXIT
command -v cc >/dev/null 2>&1 || { printf 'Install build-essential: CGO requires a C compiler.\n' >&2; exit 1; }
version=$(python3 scripts/version.py)
printf 'Building LocalWallet Go components %s with %s\n' "$version" "$(go version)"
mkdir -p bin
for component in core signer launcher; do
  printf 'Compiling wallet-%s...\n' "$component"
  go build -trimpath -ldflags="-s -w -buildid= -X localwallet/internal/buildinfo.Version=$version" -o "bin/wallet-$component" "./cmd/wallet-$component"
done
python3 - <<'PY'
import hashlib,json,pathlib,subprocess
from scripts.version import selected_version
root=pathlib.Path('bin')
manifest={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in root.glob('wallet-*') if p.is_file()}
(root/'manifest.json').write_text(json.dumps(manifest,sort_keys=True)+'\n')
(root/'go-build-manifest.json').write_text(json.dumps({'schemaVersion':1,'kind':'go-components','status':'DEVELOPMENT_UNSIGNED','version':selected_version(pathlib.Path.cwd()),'platform':'linux-amd64','source':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'dirty':bool(subprocess.check_output(['git','status','--porcelain'],text=True).strip()),'go':subprocess.check_output(['go','version'],text=True).strip(),'binaryManifest':manifest},indent=2)+'\n')
PY
printf 'Build complete: bin/wallet-core, bin/wallet-signer, bin/wallet-launcher.\nThese are Go components; the desktop interface is built separately with make build-desktop (no Debian package).\n'
