#!/usr/bin/env python3
"""Check independent public API/DTO policies; Go tests cover runtime isolation."""
import pathlib, re, subprocess

root = pathlib.Path(__file__).resolve().parents[1]
for name in ['cmd/wallet-signer/main.go', 'cmd/wallet-core/main.go']:
    source = (root / name).read_text()
    for forbidden in ['GetMnemonic', 'ExportPrivateKey', 'SignBytes', 'pprof', 'net.Listen', 'http.ListenAndServe']:
        assert forbidden not in source, (name, forbidden)
    # Removed file-backup operations must not remain callable through either
    # private dispatch or the public coordinator, including grouped cases.
    cases = re.findall(r'^\s*case\s+([^:]+):', source, re.M)
    callable_names = {value for case in cases for value in re.findall(r'"([A-Za-z][A-Za-z0-9]*)"', case)}
    assert not callable_names.intersection({'backup', 'restore', 'revealPhrase', 'exportPhrase', 'getMnemonic'}), name
    assert 'confirmRecovery' in callable_names, name

# This policy is deliberately independent of the implementation's method list:
# public state can describe recovery completion, never a recovery secret. A new
# DTO field needs explicit review here as well as its actual dispatch tests.
expected_snapshot = {'locked', 'walletExists', 'vaultState', 'environment', 'recoveryConfirmed', 'accounts', 'operations'}
for name, typename in [('internal/signer/engine.go', 'Snapshot'), ('cmd/wallet-core/main.go', 'snapshot')]:
    source = (root / name).read_text()
    dto = re.search(r'type ' + typename + r' struct\s*\{([^}]+)\}', source)
    assert dto, (name, 'missing public snapshot')
    assert set(re.findall(r'json:"([^",]+)', dto.group(1))) == expected_snapshot, (name, 'unreviewed public state fields')

desktop = (root / 'desktop/src/main.ts').read_text()
assert '--no-sandbox' not in desktop
assert 'LOCALWALLET_INHERITED_CORE' in desktop
assert 'spawn(' not in desktop
subprocess.run(['npm', '--prefix', 'desktop', 'run', 'build'], cwd=root, check=True)
# Exercise the compiled validator rather than trusting the TypeScript text.
# The only public inputs to setup are choices/IDs; phrase words and passwords
# are accepted exclusively by the dedicated ephemeral Electron prompt surface.
# The portfolio preload never receives or replies to those prompts.
subprocess.run(['node', '-e', r'''
const assert = require('node:assert/strict');
const {validate, methods} = require('./desktop/app/validation.js');
validate('confirmRecovery', {});
for (const method of ['backup','restore','getMnemonic','revealPhrase','exportPhrase','exportPrivateKey','signBytes','promptReply','prompt']) {
  assert.ok(!methods.includes(method), method);
  assert.throws(() => validate(method, {}), method);
}
for (const method of ['create','import','unlock','confirmRecovery']) {
  const base = method === 'create' || method === 'import' ? {words:12} : {};
  for (const key of ['password','phrase','mnemonic','passphrase','seed','privateKey','recordedWords']) {
    assert.throws(() => validate(method, {...base, [key]:'PUBLIC-SYNTHETIC-FORBIDDEN'}), method + ':' + key);
  }
}
'''], cwd=root, check=True)
subprocess.run(['npm', '--prefix', 'desktop', 'test'], cwd=root, check=True)
# The secret-bearing prompt bridge is intentionally separate from the portfolio
# bridge. It has no generic request, file, clipboard-value or signing capability.
prompt_preload = (root / 'desktop/src/prompt-preload.ts').read_text()
assert "exposeInMainWorld('walletPrompt'" in prompt_preload
for forbidden in ['wallet:request', 'wallet:version', 'readFile', 'process.env', 'writeText', 'signBytes']:
    assert forbidden not in prompt_preload, forbidden
assert "const promptOrigin='wallet://prompt'" in desktop
assert "'wallet-prompt-'+randomBytes(16)" in desktop
assert "preload:path.join(__dirname,'prompt-preload.js')" in desktop
assert 'clearStorageData()' in desktop
portfolio_preload = (root / 'desktop/src/preload.ts').read_text()
assert 'wallet:prompt' not in portfolio_preload
assert 'promptReply' not in portfolio_preload
print('PASS public API/DTO policy, isolated Electron prompt boundary and regression checks')
