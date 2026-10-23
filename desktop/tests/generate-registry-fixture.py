#!/usr/bin/env python3
"""Refresh public UI DTO fixtures from the actual Go registry, without keys or RPC.
Run ./scripts/with-go.sh python3 desktop/tests/generate-registry-fixture.py.
This is registry synchronization, not an independent signing interoperability oracle.
"""
import hashlib
import json
import pathlib
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[2]
(root / '.tools').mkdir(exist_ok=True)
with tempfile.TemporaryDirectory(prefix='public-ui-registry-', dir=root / '.tools') as temporary:
    source = pathlib.Path(temporary) / 'main.go'
    source.write_text('''package main
import("encoding/json";"os";"localwallet/internal/chains";"localwallet/internal/model")
func main(){var all []model.Network;for _,a:=range chains.All(){for _,n:=range a.Networks(){all=append(all,n)}};if e:=json.NewEncoder(os.Stdout).Encode(all);e!=nil{panic(e)}}
''')
    networks = json.loads(subprocess.check_output(['go', 'run', str(source)], cwd=root, text=True))
files = ['internal/chains/registry.go', 'internal/chains/evm/registry.go', 'internal/chains/hypercore/adapter.go', 'internal/chains/near/near.go', 'internal/chains/aptos/aptos.go', 'internal/chains/decred/decred.go', 'internal/chains/ton/ton.go']
for environment in ['testnet', 'mainnet']:
    selected = [network for network in networks if network['testnet'] == (environment == 'testnet')]
    fixture = {
        'schemaVersion': 1,
        'scope': 'Public network DTOs from chains.All().Networks(), filtered by the named environment; no account seed, native UI, RPC or transaction operation.',
        'sourceSHA256': {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in files},
        'networks': selected,
    }
    (root / f'desktop/tests/fixtures/networks-{environment}.json').write_text(json.dumps(fixture, indent=2) + '\n')
    print('Public registry fixture:', environment, len(selected), 'networks,', sum(len(n['assets']) for n in selected), 'assets')
