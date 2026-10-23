#!/usr/bin/env python3
"""Check enumerated production surfaces; runtime isolation is tested by Go."""
import pathlib,subprocess
root=pathlib.Path(__file__).resolve().parents[1]
text=(root/'cmd/wallet-signer/main.go').read_text()
for forbidden in ['GetMnemonic','ExportPrivateKey','SignBytes','pprof','net.Listen','http.ListenAndServe']:
    assert forbidden not in text,forbidden
desktop=(root/'desktop/src/main.ts').read_text()
assert '--no-sandbox' not in desktop
assert "LOCALWALLET_INHERITED_CORE" in desktop
assert "spawn(" not in desktop
subprocess.run(['npm','--prefix','desktop','run','build'],cwd=root,check=True)
subprocess.run(['npm','--prefix','desktop','test'],cwd=root,check=True)
print('PASS production API enumeration and Electron configuration regression checks')
