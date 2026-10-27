import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {createHash, generateKeyPairSync, sign, verify} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {signRelease} from './attest.mjs';

// Transport stubs are not publisher signatures. Existing Python regressions
// separately verify real frozen upstream signatures with the actual gh CLI.
const unsignedTransportFixture = {dsseEnvelope: {payload: 'public-test-only'}, verificationMaterial: {}};

async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'wallet-private-provenance-'));
  t.after(() => fs.rm(root, {recursive: true, force: true}));
  const dist = path.join(root, 'dist'); await fs.mkdir(dist);
  const env = {GITHUB_ACTIONS: 'true', RUNNER_ENVIRONMENT: 'github-hosted',
    GITHUB_SERVER_URL: 'https://github.com', GITHUB_API_URL: 'https://api.github.com',
    GITHUB_EVENT_NAME: 'push', GITHUB_REPOSITORY: 'fixture/wallet', GITHUB_REPOSITORY_ID: '123',
    GITHUB_SHA: 'a'.repeat(40), GITHUB_REF: 'refs/heads/main', GITHUB_WORKSPACE: root,
    GITHUB_WORKFLOW_REF: 'fixture/wallet/.github/workflows/release.yml@refs/heads/main'};
  const metadata = {publisherRepository: env.GITHUB_REPOSITORY, publisherRepositoryId: env.GITHUB_REPOSITORY_ID,
    sourceRef: env.GITHUB_REF, sourceCommit: env.GITHUB_SHA, signerWorkflow: '.github/workflows/release.yml'};
  await fs.writeFile(path.join(dist, 'release.json'), JSON.stringify(metadata));
  await fs.writeFile(path.join(dist, 'RELEASE-SHA256SUMS'), 'public synthetic manifest\n');
  return {root, dist, env, metadata, output: path.join(dist, 'release-provenance.sigstore.json')};
}

test('exact manifest digest uses private Sigstore and skips storage without a repository token', async t => {
  const f = await fixture(t); let calls = 0;
  await signRelease(f.root, f.env, async options => {
    calls++;
    assert.deepEqual(options, {subjects: [{name: 'RELEASE-SHA256SUMS', digest: {
      sha256: createHash('sha256').update('public synthetic manifest\n').digest('hex')}}],
      skipWrite: true, sigstore: 'github', token: ''});
    return {bundle: unsignedTransportFixture};
  });
  assert.equal(calls, 1);
  assert.deepEqual(JSON.parse(await fs.readFile(f.output)), unsignedTransportFixture);
  assert.equal((await fs.stat(f.output)).mode & 0o777, 0o600);
});

test('non-CI, non-hosted, foreign origin, PR, wrong workflow and malformed identity never sign', async t => {
  const f = await fixture(t);
  for (const change of [{GITHUB_ACTIONS: ''}, {RUNNER_ENVIRONMENT: 'self-hosted'},
    {GITHUB_SERVER_URL: 'https://other.invalid'}, {GITHUB_API_URL: 'https://other.invalid'},
    {GITHUB_EVENT_NAME: 'pull_request'}, {GITHUB_REPOSITORY: '../wallet'}, {GITHUB_REPOSITORY_ID: '0'},
    {GITHUB_REF: 'refs/pull/1/merge'}, {GITHUB_SHA: 'a'.repeat(39)}, {GITHUB_WORKSPACE: '.'},
    {GITHUB_WORKSPACE: os.tmpdir()}, {GITHUB_WORKFLOW_REF: 'fixture/wallet/.github/workflows/ci.yml@refs/heads/main'}]) {
    await assert.rejects(signRelease(f.root, {...f.env, ...change}, () => assert.fail('must not request identity')));
  }
});

test('release metadata must match every expected publisher/source field', async t => {
  const f = await fixture(t);
  for (const key of Object.keys(f.metadata)) {
    await fs.writeFile(path.join(f.dist, 'release.json'), JSON.stringify({...f.metadata, [key]: 'foreign'}));
    await assert.rejects(signRelease(f.root, f.env, () => assert.fail('must not sign foreign metadata')));
  }
});

test('manifest symlinks, hard links, empty and oversized inputs are rejected before signing', async t => {
  const f = await fixture(t); const manifest = path.join(f.dist, 'RELEASE-SHA256SUMS');
  const target = path.join(f.root, 'outside'); await fs.writeFile(target, 'public fixture');
  for (const kind of ['symlink', 'hardlink', 'empty', 'oversized']) {
    await fs.unlink(manifest);
    if (kind === 'symlink') await fs.symlink(target, manifest);
    else if (kind === 'hardlink') await fs.link(target, manifest);
    else await fs.writeFile(manifest, kind === 'empty' ? '' : 'x'.repeat(65537));
    await assert.rejects(signRelease(f.root, f.env, () => assert.fail('unsafe manifest must not sign')));
  }
});

test('directory symlink is rejected before signing', async t => {
  const f = await fixture(t);
  await fs.rename(f.dist, path.join(f.root, 'moved'));
  await fs.symlink(path.join(f.root, 'moved'), f.dist);
  await assert.rejects(signRelease(f.root, f.env, () => assert.fail('unsafe directory must not sign')));
});

test('existing bundle and output symlink are never overwritten', async t => {
  const f = await fixture(t); const target = path.join(f.root, 'target');
  await fs.writeFile(f.output, 'preserved');
  await assert.rejects(signRelease(f.root, f.env, () => assert.fail('existing output must not sign')));
  await fs.unlink(f.output); await fs.writeFile(target, 'preserved'); await fs.symlink(target, f.output);
  await assert.rejects(signRelease(f.root, f.env, () => assert.fail('output symlink must not sign')));
  assert.equal(await fs.readFile(target, 'utf8'), 'preserved');
});

test('manifest or metadata change during signing prevents bundle persistence', async t => {
  for (const name of ['RELEASE-SHA256SUMS', 'release.json']) {
    const f = await fixture(t);
    await assert.rejects(signRelease(f.root, f.env, async () => {
      await fs.writeFile(path.join(f.dist, name), name === 'release.json' ? '{}' : 'changed');
      return {bundle: unsignedTransportFixture};
    }));
    await assert.rejects(fs.stat(f.output), {code: 'ENOENT'});
  }
});

test('network/signing failure has no unsigned fallback', async t => {
  const f = await fixture(t);
  await assert.rejects(signRelease(f.root, f.env, async () => {throw new Error('public test failure');}));
  await assert.rejects(fs.stat(f.output), {code: 'ENOENT'});
});

test('missing or oversized returned bundle cannot be persisted', async t => {
  for (const result of [{}, {bundle: {}}, {bundle: {...unsignedTransportFixture, padding: 'x'.repeat(16 * 1024 * 1024)}}]) {
    const f = await fixture(t);
    await assert.rejects(signRelease(f.root, f.env, async () => result));
    await assert.rejects(fs.stat(f.output), {code: 'ENOENT'});
  }
});

test('output created while signing is preserved by exclusive creation', async t => {
  const f = await fixture(t);
  await assert.rejects(signRelease(f.root, f.env, async () => {
    await fs.writeFile(f.output, 'preserved'); return {bundle: unsignedTransportFixture};
  }), {code: 'EEXIST'});
  assert.equal(await fs.readFile(f.output, 'utf8'), 'preserved');
});

test('CLI outside hosted CI fails without printing environment credentials or creating a bundle', () => {
  try {
    execFileSync(process.execPath, [new URL('./attest.mjs', import.meta.url).pathname],
      {env: {GITHUB_ACTIONS: 'false', GH_TOKEN: 'public-token-canary'}, stdio: 'pipe'});
    assert.fail('CLI should fail outside hosted CI');
  } catch (error) {
    assert.equal(error.status, 1);
    assert.equal(error.stdout.toString(), '');
    assert.equal(error.stderr.toString(), 'Release provenance signing failed; no release was published.\n');
  }
});

test('actual upstream toolkit skips storage; its storage-enabled control fails as expected', async t => {
  const {bundleFromJSON} = await import('@sigstore/bundle');
  const frozen = JSON.parse(await fs.readFile(new URL('../../tests/release-auth/sigstore-js-2.1.0-bundle.json', import.meta.url)));
  let storageCalls = 0;
  const signer = t.mock.module(new URL('./node_modules/@actions/attest/lib/sign.js', import.meta.url).href,
    {namedExports: {signPayload: async () => bundleFromJSON(frozen)}});
  const store = t.mock.module(new URL('./node_modules/@actions/attest/lib/store.js', import.meta.url).href,
    {namedExports: {writeAttestation: async () => {storageCalls++; throw new Error('storage unavailable');}}});
  t.after(() => {signer.restore(); store.restore();});
  const {attest} = await import('./node_modules/@actions/attest/lib/attest.js?skip-storage-regression');
  const options = {subjects: [{name: 'public-fixture', digest: {sha256: 'a'.repeat(64)}}], sigstore: 'github', token: ''};
  const result = await attest({...options, skipWrite: true});
  assert.ok(result.bundle.dsseEnvelope); assert.equal(result.attestationID, undefined); assert.equal(storageCalls, 0);
  await assert.rejects(attest({...options, skipWrite: false}), /storage unavailable/);
  assert.equal(storageCalls, 1);
});

test('pinned Sigstore core keeps DSSE UTF-8 types bound through the actual bundle builder', async () => {
  const {DSSEBundleBuilder} = await import('@sigstore/sign');
  const builder = new DSSEBundleBuilder({signer: {sign() {assert.fail('no service signing');}}, witnesses: []});
  const data = Buffer.from('public test data');
  const plain = await builder.prepare({data, type: 'text/plain'});
  const unicode = await builder.prepare({data, type: '\u0174ext/plain'});
  assert.equal(plain.toString(), 'DSSEv1 10 text/plain 16 public test data');
  assert.notDeepEqual(plain, unicode);
  const {privateKey, publicKey} = generateKeyPairSync('ec', {namedCurve: 'P-256'});
  const signature = sign('sha256', plain, privateKey);
  assert.ok(verify('sha256', plain, publicKey, signature));
  assert.equal(verify('sha256', unicode, publicKey, signature), false);
});
