// CI-only release signing. No wallet keys, public transparency-log upload,
// attestation storage API, long-lived signing key or unsigned fallback.
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';

const MANIFEST = 'RELEASE-SHA256SUMS';
const OUTPUT = 'release-provenance.sigstore.json';
const WORKFLOW = '.github/workflows/release.yml';

async function regular(filename, limit) {
  const stat = await fs.lstat(filename);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.nlink !== 1 || stat.size < 1 || stat.size > limit) {
    throw new Error('Unsafe release signing input');
  }
  return fs.readFile(filename);
}

export async function validateContext(root, env) {
  if (env.GITHUB_ACTIONS !== 'true' || env.RUNNER_ENVIRONMENT !== 'github-hosted' ||
      env.GITHUB_SERVER_URL !== 'https://github.com' || env.GITHUB_API_URL !== 'https://api.github.com' ||
      !['push', 'workflow_dispatch'].includes(env.GITHUB_EVENT_NAME) ||
      !/^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9_.-]{1,100}$/.test(env.GITHUB_REPOSITORY ?? '') ||
      !/^[1-9][0-9]{0,19}$/.test(env.GITHUB_REPOSITORY_ID ?? '') ||
      !/^refs\/heads\/(main|master)$/.test(env.GITHUB_REF ?? '') ||
      !/^[0-9a-f]{40}$/.test(env.GITHUB_SHA ?? '') ||
      env.GITHUB_WORKFLOW_REF !== `${env.GITHUB_REPOSITORY}/${WORKFLOW}@${env.GITHUB_REF}` ||
      !env.GITHUB_WORKSPACE || !path.isAbsolute(env.GITHUB_WORKSPACE) ||
      await fs.realpath(env.GITHUB_WORKSPACE) !== await fs.realpath(root)) {
    throw new Error('Release signing requires the matching GitHub-hosted release workflow');
  }
  const dist = path.join(root, 'dist');
  if ((await fs.lstat(dist)).isSymbolicLink() || !(await fs.stat(dist)).isDirectory()) {
    throw new Error('Unsafe release directory');
  }
  const metadata = JSON.parse(await regular(path.join(dist, 'release.json'), 65536));
  const expected = {publisherRepository: env.GITHUB_REPOSITORY, publisherRepositoryId: env.GITHUB_REPOSITORY_ID,
    sourceRef: env.GITHUB_REF, sourceCommit: env.GITHUB_SHA, signerWorkflow: WORKFLOW};
  if (Object.entries(expected).some(([key, value]) => metadata[key] !== value)) {
    throw new Error('Release signing identity differs');
  }
  return dist;
}

export async function signRelease(root, env, attestProvenance) {
  const dist = await validateContext(root, env);
  const subject = path.join(dist, MANIFEST);
  const before = await regular(subject, 65536);
  const digest = createHash('sha256').update(before).digest('hex');
  const output = path.join(dist, OUTPUT);
  // Never overwrite an existing bundle or follow its symlink.
  try { await fs.lstat(output); throw new Error('Release bundle already exists'); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
  const result = await attestProvenance({
    subjects: [{name: MANIFEST, digest: {sha256: digest}}],
    skipWrite: true,
    sigstore: 'github', // GitHub's private CA/timestamp service; never public Rekor.
    token: '', // No repository write token is needed when storage is disabled.
  });
  if (!(await regular(subject, 65536)).equals(before)) throw new Error('Release manifest changed while signing');
  await validateContext(root, env);
  if (!result?.bundle?.dsseEnvelope || !result.bundle.verificationMaterial) throw new Error('Signing returned no bundle');
  const serialized = JSON.stringify(result.bundle) + '\n';
  if (Buffer.byteLength(serialized) > 16 * 1024 * 1024) throw new Error('Oversized release bundle');
  await fs.writeFile(output, serialized, {flag: 'wx', mode: 0o600});
  return output;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const root = fileURLToPath(new URL('../../', import.meta.url));
    // Validate before loading any signing/identity client or requesting a token.
    await validateContext(root, process.env);
    const {attestProvenance} = await import('@actions/attest');
    await signRelease(root, process.env, attestProvenance);
    console.log('Signed detached release provenance; GitHub attestation storage was not used.');
  } catch {
    // Upstream/network exceptions can contain credential-bearing diagnostics.
    console.error('Release provenance signing failed; no release was published.');
    process.exitCode = 1;
  }
}
