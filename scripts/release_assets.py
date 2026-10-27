#!/usr/bin/env python3
"""Strict complete-release inventory and publisher-provenance verification.

No artifact is executed or extracted. The expected publisher identity, source
commit, branch and release version must come from the consumer, not the download.
Cryptographic Sigstore/certificate/transparency verification belongs to GitHub CLI.
"""
import argparse
import hashlib
import json
import pathlib
import re
import subprocess

from version import validate_version

MANIFEST = 'RELEASE-SHA256SUMS'
BUNDLE = 'release-provenance.sigstore.json'
METADATA = 'release.json'
WORKFLOW = '.github/workflows/release.yml'
MIN_GH = (2, 103, 2)


# Authentication records are retained as an Actions artifact. Only the portable
# desktop archive is uploaded to GitHub Releases.
def expected_assets(version):
    validate_version(version)
    return sorted(('RELEASE-NOTES.md', METADATA,
                   f'cryptowallet-{version}-linux-amd64.tar.gz'))


def identity(repo, ref, commit, repo_id=None):
    if not isinstance(repo, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}', repo) or repo.endswith('/.') or repo.endswith('/..'):
        raise ValueError('Expected GitHub publisher repository is invalid')
    if ref not in ('refs/heads/main', 'refs/heads/master'):
        raise ValueError('Expected protected source branch must be main or master')
    if not isinstance(commit, str) or not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('Expected source commit must be its full lowercase SHA-1')
    if repo_id is not None and (not isinstance(repo_id, str) or not re.fullmatch(r'[1-9][0-9]{0,19}', repo_id)):
        raise ValueError('Expected immutable GitHub repository ID is invalid')


def regular(directory, name, limit=None):
    directory = pathlib.Path(directory)
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError('Release directory must be a real directory')
    path = directory / name
    if path.is_symlink() or not path.is_file() or path.stat().st_nlink != 1:
        raise ValueError('Missing or unsafe release asset: ' + name)
    if limit is not None and path.stat().st_size > limit:
        raise ValueError('Oversized release metadata: ' + name)
    return path


def digest(path):
    with pathlib.Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def unique_json(content):
    def unique(pairs):
        out = {}
        for key, value in pairs:
            if key in out:
                raise ValueError('Duplicate release metadata member')
            out[key] = value
        return out
    return json.loads(content, object_pairs_hook=unique)


def metadata(version, repo, ref, commit, repo_id=None):
    validate_version(version)
    identity(repo, ref, commit, repo_id)
    return {'schemaVersion': 1, 'version': version, 'tag': version,
            'platform': 'linux-amd64', 'publisherRepository': repo,
            'publisherRepositoryId': repo_id,
            'sourceRef': ref, 'sourceCommit': commit, 'signerWorkflow': WORKFLOW,
            'authentication': 'github-oidc-sigstore-provenance',
            'productionReadiness': 'NOT_RUN: mandatory external gates remain incomplete'}


def prepare(directory, version, repo, ref, commit, repo_id=None):
    """Generate only checksums and identity metadata; this does not sign anything."""
    directory = pathlib.Path(directory)
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError('Release directory must be a real directory')
    data = metadata(version, repo, ref, commit, repo_id)
    for name in (METADATA, MANIFEST):
        path = directory / name
        if path.is_symlink() or (path.exists() and (not path.is_file() or path.stat().st_nlink != 1)):
            raise ValueError('Unsafe release metadata target')
    # Require every payload before writing the candidate manifest. No globs or
    # optional desktop path can silently downgrade a complete-wallet release.
    for name in expected_assets(version):
        if name != METADATA:
            regular(directory, name)
    (directory / METADATA).write_text(json.dumps(data, indent=2) + '\n')
    content = ''.join(digest(regular(directory, name)) + '  ' + name + '\n'
                      for name in expected_assets(version))
    (directory / MANIFEST).write_text(content)
    return content


def check(directory, version, repo, ref, commit, repo_id=None):
    """Integrity only; callers must never report this operation as authenticated."""
    expected = metadata(version, repo, ref, commit, repo_id)
    manifest = regular(directory, MANIFEST, 65536).read_text(encoding='ascii')
    entries = {}
    for line in manifest.splitlines():
        found = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9][A-Za-z0-9_.-]{0,199})', line)
        if not found or found[2] in entries:
            raise ValueError('Malformed, duplicate or unsafe release checksum entry')
        entries[found[2]] = found[1]
    if set(entries) != set(expected_assets(version)):
        raise ValueError('Release checksum manifest is incomplete or has unexpected assets')
    for name, wanted in entries.items():
        if digest(regular(directory, name)) != wanted:
            raise ValueError('Release asset checksum differs: ' + name)
    actual = unique_json(regular(directory, METADATA, 65536).read_text())
    if actual != expected or type(actual.get('schemaVersion')) is not int:
        raise ValueError('Release identity/version differs from the expected publisher candidate')
    return entries


def verify_provenance(artifact, bundle, repo, ref, commit, gh='gh', trusted_root=None, digest_algorithm='sha256', repo_id=None):
    """Authenticate a local subject with exact externally supplied identity policy."""
    identity(repo, ref, commit, repo_id)
    result = subprocess.run([str(gh), 'version'], text=True, capture_output=True, check=True, timeout=15)
    match = re.match(r'gh version ([0-9]+)\.([0-9]+)\.([0-9]+)(?:\s|$)', result.stdout)
    if not match or tuple(map(int, match.groups())) < MIN_GH:
        raise ValueError('GitHub CLI 2.103.2 or later is required for release authentication')
    if digest_algorithm not in ('sha256', 'sha512'):
        raise ValueError('Unsupported attestation digest')
    command = [str(gh), 'attestation', 'verify', str(artifact), '--bundle', str(bundle), '--digest-alg', digest_algorithm,
               '--repo', repo,
               '--cert-identity', f'https://github.com/{repo}/{WORKFLOW}@{ref}',
               '--cert-oidc-issuer', 'https://token.actions.githubusercontent.com',
               '--source-ref', ref, '--source-digest', commit,
               '--deny-self-hosted-runners', '--predicate-type', 'https://slsa.dev/provenance/v1', '--format', 'json']
    if trusted_root is not None:
        command += ['--custom-trusted-root', str(trusted_root)]
    result = subprocess.run(command, check=True, timeout=180, text=True, capture_output=True)
    verified = unique_json_result(result.stdout)
    if repo_id is not None and not any(item.get('verificationResult', {}).get('signature', {}).get('certificate', {}).get('sourceRepositoryIdentifier') == repo_id for item in verified):
        raise ValueError('Verified immutable publisher repository ID differs')


def unique_json_result(content):
    result = unique_json(content)
    if not isinstance(result, list) or not result or any(not isinstance(item, dict) for item in result):
        raise ValueError('GitHub CLI returned no verified attestation')
    return result


def verify(directory, version, repo, ref, commit, gh='gh', trusted_root=None, repo_id=None):
    if repo_id is None:
        raise ValueError('An independently trusted immutable publisher repository ID is required')
    manifest = regular(directory, MANIFEST, 65536)
    bundle = regular(directory, BUNDLE, 16 * 1024 * 1024)
    before = digest(manifest)
    verify_provenance(manifest, bundle, repo, ref, commit, gh, trusted_root, repo_id=repo_id)
    if digest(regular(directory, MANIFEST, 65536)) != before:
        raise ValueError('Release checksum manifest changed during authentication')
    return check(directory, version, repo, ref, commit, repo_id)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=('prepare', 'check', 'verify'))
    parser.add_argument('--directory', type=pathlib.Path, default=pathlib.Path('dist'))
    parser.add_argument('--version', required=True)
    parser.add_argument('--repo', required=True, help='independently trusted owner/repository')
    parser.add_argument('--repo-id', required=True, help='independently trusted immutable numeric GitHub repository ID')
    parser.add_argument('--ref', required=True, help='independently trusted protected branch ref')
    parser.add_argument('--commit', required=True, help='independently trusted full source commit')
    parser.add_argument('--gh', default='gh', help='trusted GitHub CLI executable')
    parser.add_argument('--trusted-root', type=pathlib.Path, help='optional independently trusted offline Sigstore root')
    args = parser.parse_args()
    try:
        if args.operation == 'verify':
            verify(args.directory, args.version, args.repo, args.ref, args.commit, args.gh, args.trusted_root, args.repo_id)
            print('PASS authenticated complete release and exact expected version/source identity')
        else:
            globals()[args.operation](args.directory, args.version, args.repo, args.ref, args.commit, args.repo_id)
            print('PASS complete release inventory; publisher authentication has NOT been performed')
    except (OSError, ValueError, TypeError, subprocess.SubprocessError) as error:
        raise SystemExit('Release verification failed: ' + str(error))


if __name__ == '__main__':
    main()
