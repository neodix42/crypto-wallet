#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
trap 'code=$?; if [ "$code" -ne 0 ]; then printf "Build failed (exit %s). See the command/error above; prerequisites are documented in README.md.\n" "$code" >&2; fi' EXIT
command -v cc >/dev/null 2>&1 || { printf 'Install build-essential: the native GTK desktop uses CGO.\n' >&2; exit 1; }
version=$(python3 scripts/version.py)
mkdir -p bin
if [ "${1:-}" = --components ]; then
  printf 'Building legacy CryptoWallet Go components %s with %s\n' "$version" "$(go version)"
  for component in core signer launcher; do
    printf 'Compiling wallet-%s...\n' "$component"
    go build -trimpath -ldflags="-s -w -buildid= -X localwallet/internal/buildinfo.Version=$version" -o "bin/wallet-$component" "./cmd/wallet-$component"
  done
  python3 - <<'PY'
import hashlib,json,pathlib,subprocess
from scripts.version import selected_version
root=pathlib.Path('bin')
names=('wallet-core','wallet-signer','wallet-launcher')
manifest={name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in names}
(root/'manifest.json').write_text(json.dumps(manifest,sort_keys=True)+'\n')
(root/'go-build-manifest.json').write_text(json.dumps({'schemaVersion':1,'kind':'go-components','status':'DEVELOPMENT_UNSIGNED','version':selected_version(pathlib.Path.cwd()),'platform':'linux-amd64','source':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'dirty':bool(subprocess.check_output(['git','status','--porcelain'],text=True).strip()),'go':subprocess.check_output(['go','version'],text=True).strip(),'binaryManifest':manifest},indent=2)+'\n')
PY
  printf 'Legacy components: bin/wallet-core, bin/wallet-signer, bin/wallet-launcher.\n'
  exit 0
fi
[ "$#" -eq 0 ] || { printf 'Unknown build argument.\n' >&2; exit 2; }
printf 'Building portable CryptoWallet %s with %s\n' "$version" "$(go version)"
# Notices are embedded in the executable; no sidecar license file is needed.
python3 scripts/license-notices.py --portable
CGO_ENABLED=1 go build -tags portable -trimpath -ldflags="-s -w -buildid= -X localwallet/internal/buildinfo.Version=$version" -o bin/cryptowallet ./cmd/cryptowallet
python3 - <<'PY'
import hashlib,json,pathlib,subprocess
from scripts.version import selected_version
root=pathlib.Path.cwd(); binary=root/'bin/cryptowallet'
manifest={'schemaVersion':1,'kind':'portable-go-wallet','version':selected_version(root),'platform':'linux-amd64','file':'cryptowallet','sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'source':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'dirty':bool(subprocess.check_output(['git','status','--porcelain'],text=True).strip()),'go':subprocess.check_output(['go','version'],text=True).strip(),'buildTags':['portable'],'ui':'native-gtk3','sandbox':'none','runtimeDependencies':['libc6','libgtk-3-0t64','ca-certificates'],'licenses':'embedded; cryptowallet --licenses'}
(root/'bin/portable-build-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
PY
printf 'Build complete: bin/cryptowallet (one native Go executable; system GTK3 required).\nNo Electron, Chromium, sandbox helper or .deb is built.\n'
