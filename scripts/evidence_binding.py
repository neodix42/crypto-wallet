#!/usr/bin/env python3
"""Candidate-bound evidence checks. No profiles or downloaded code are executed."""
import hashlib
import json
import math
import os
import pathlib
import re

COMPONENTS = ('wallet-core', 'wallet-signer', 'wallet-launcher')
PRODUCTION_FILES = ('app/codec.js', 'app/main.js', 'app/preload.js',
                    'app/prompt-preload.js', 'app/prompts.js', 'app/validation.js',
                    'ui/index.html', 'ui/prompt.css', 'ui/prompt.html',
                    'ui/prompt.js', 'ui/renderer.js', 'ui/style.css')
FRONTEND_PINS = ('desktop/package.json', 'desktop/package-lock.json', 'desktop/tsconfig.json',
                 'scripts/frontend-coverage.py', 'scripts/evidence_binding.py')
AUTH_INPUTS = ('scripts/release_assets.py', 'scripts/release-auth-test.py',
               'scripts/bootstrap-gh.py', '.github/workflows/release.yml',
               '.github/provenance/attest.mjs', '.github/provenance/attest.test.mjs',
               '.github/provenance/package.json', '.github/provenance/package-lock.json',
               'tests/release-auth/provenance.json')
EVIDENCE_INPUTS = ('scripts/evidence_binding.py', 'scripts/verify-release.py',
                   'scripts/frontend-coverage.py', 'scripts/verify-artifact.cjs',
                   'scripts/release-evidence-test.py')
EXCLUDED = {'.git', '.tools', 'node_modules', 'dist', 'bin', 'reports', 'wallet-runtime', '__pycache__'}


class EvidenceError(ValueError):
    """Only fixed, nonsecret error codes leave the release verifier."""


def regular(root, name, limit=None):
    if not isinstance(name, str) or not name or pathlib.PurePosixPath(name).is_absolute() or '..' in pathlib.PurePosixPath(name).parts:
        raise EvidenceError('unsafe-evidence-path')
    path = pathlib.Path(root) / name
    # Check every component: a safe-looking report must not follow an external
    # directory symlink, even if the final leaf itself is a regular file.
    current = pathlib.Path(root)
    for part in pathlib.PurePosixPath(name).parts:
        current /= part
        if current.is_symlink():
            raise EvidenceError('symlinked-evidence-file')
    if not path.is_file() or (limit is not None and path.stat().st_size > limit):
        raise EvidenceError('missing-or-oversized-evidence-file')
    return path


def sha256(path):
    with pathlib.Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def read_json(root, name):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise EvidenceError('duplicate-evidence-member')
            value[key] = item
        return value
    try:
        value = json.loads(regular(root, name, 32 * 1024 * 1024).read_text(), object_pairs_hook=unique)
    except (json.JSONDecodeError, UnicodeError):
        raise EvidenceError('invalid-evidence-json') from None
    if not isinstance(value, dict):
        raise EvidenceError('invalid-evidence-object')
    return value


def files_below(root, folder, suffix=None):
    base = pathlib.Path(root) / folder
    if not base.is_dir() or base.is_symlink():
        raise EvidenceError('missing-or-unsafe-source-directory')
    names = []
    for directory, folders, files in os.walk(base, followlinks=False):
        folders[:] = sorted(name for name in folders if name not in EXCLUDED)
        for name in folders:
            if (pathlib.Path(directory) / name).is_symlink():
                raise EvidenceError('symlinked-source-directory')
        for name in sorted(files):
            relative = (pathlib.Path(directory) / name).relative_to(root).as_posix()
            if suffix is None or relative.endswith(suffix):
                regular(root, relative)
                names.append(relative)
    return names


def hash_names(root, names):
    return {name: sha256(regular(root, name)) for name in sorted(set(names))}


def go_sources(root):
    names = files_below(root, '.', '.go')
    return hash_names(root, [*names, 'go.mod', 'go.sum'])


def frontend_sources(root):
    names = list(FRONTEND_PINS)
    for folder in ('desktop/src', 'desktop/ui', 'desktop/tests'):
        names.extend(files_below(root, folder))
    return hash_names(root, names)


def frontend_runtime(root):
    names = files_below(root, 'desktop/app', '.js') + files_below(root, 'desktop/ui', '.js')
    return hash_names(root, names)


def equal_snapshot(actual, expected, code):
    if not isinstance(actual, dict) or not actual or actual != expected:
        raise EvidenceError(code)


def validate_source_report(root, report, names):
    if report.get('status') != 'PASS':
        raise EvidenceError('source-bound-report-incomplete')
    equal_snapshot(report.get('sourceSHA256'), hash_names(root, names), 'source-bound-report-stale-or-incomplete')


def candidate(root):
    manifest = read_json(root, 'dist/build-manifest.json')
    binaries = read_json(root, 'dist/LocalWallet-linux-x64/resources/bin/manifest.json')
    if set(binaries) != set(COMPONENTS) or any(not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{64}', value) for value in binaries.values()):
        raise EvidenceError('invalid-candidate-binary-manifest')
    if manifest.get('binaryManifest') != binaries:
        raise EvidenceError('candidate-binary-manifest-mismatch')
    for name in COMPONENTS:
        for prefix in ('bin/', 'dist/LocalWallet-linux-x64/resources/bin/'):
            if sha256(regular(root, prefix + name)) != binaries[name]:
                raise EvidenceError('candidate-binary-hash-mismatch')
    if sha256(regular(root, 'dist/LocalWallet-linux-x64/wallet-launcher')) != binaries['wallet-launcher']:
        raise EvidenceError('candidate-launcher-hash-mismatch')
    version = manifest.get('version')
    if not isinstance(version, str) or not re.fullmatch(r'(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)', version):
        raise EvidenceError('invalid-candidate-version')
    return {'version': version, 'binaryManifest': binaries,
            'asarSHA256': sha256(regular(root, 'dist/LocalWallet-linux-x64/resources/app.asar')),
            'electronSHA256': sha256(regular(root, 'dist/LocalWallet-linux-x64/LocalWallet'))}


def validate_artifact(root, report, current):
    if report.get('status') != 'PASS' or report.get('artifact') != current:
        raise EvidenceError('artifact-security-not-bound-to-candidate')
    sources = hash_names(root, ['desktop/' + name for name in PRODUCTION_FILES])
    equal_snapshot(report.get('productionSourceSHA256'), sources, 'artifact-security-stale-source')
    payloads = {name: sources['desktop/' + name] for name in PRODUCTION_FILES}
    equal_snapshot(report.get('payloadSHA256'), payloads, 'artifact-security-payload-mismatch')
    if report.get('productionPayloadMatchesSource') is not True or report.get('productionFilesCompared') != len(PRODUCTION_FILES):
        raise EvidenceError('artifact-security-payload-comparison-missing')
    fuses = report.get('fuses')
    if not isinstance(fuses, dict) or fuses.get('version') != '1' or any(type(fuses.get(key)) is not int or fuses[key] != value for key, value in {'0': 48, '1': 49, '2': 48, '3': 48, '5': 49}.items()):
        raise EvidenceError('artifact-security-fuses-incomplete')
    if set(report.get('files', [])) != {'/app', '/ui', '/package.json', *('/' + name for name in PRODUCTION_FILES)}:
        raise EvidenceError('artifact-security-inventory-incomplete')


def validate_packaged(report, current):
    expected = {key: value for key, value in current.items() if key != 'electronSHA256'}
    if report.get('status') != 'PASS' or report.get('artifact') != expected:
        raise EvidenceError('packaged-recovery-not-bound-to-candidate')
    steps = report.get('steps')
    if not isinstance(steps, list) or not steps or any(not isinstance(row, dict) or row.get('status') != 'PASS' for row in steps):
        raise EvidenceError('packaged-recovery-steps-incomplete')


def frontend_rows(root, summary):
    runtime = frontend_runtime(root)
    required = {'desktop/' + name for name in PRODUCTION_FILES if name.endswith('.js')}
    rows = []
    for name in sorted(set(runtime) | required):
        relative = name.removeprefix('desktop/')
        value = summary.get(str(pathlib.Path(root) / name))
        critical = relative.startswith('app/') or relative == 'ui/prompt.js'
        statement_floor, branch_floor = (95, 90) if critical else (90, 85)
        statements = value.get('statements', {}).get('pct') if isinstance(value, dict) else None
        branches = value.get('branches', {}).get('pct') if isinstance(value, dict) else None
        def percentage(item):
            return type(item) in (int, float) and math.isfinite(item) and 0 <= item <= 100
        passed = name in runtime and percentage(statements) and percentage(branches) and statements >= statement_floor and branches >= branch_floor
        rows.append({'file': relative, 'statements': statements, 'branches': branches,
                     'floors': {'statements': statement_floor, 'branches': branch_floor},
                     'status': 'PASS' if passed else 'FAIL'})
    return rows


def validate_frontend(root, report):
    if report.get('status') != 'PASS' or report.get('sourceStable') is not True:
        raise EvidenceError('frontend-coverage-missing-or-unstable')
    equal_snapshot(report.get('sourceSHA256'), frontend_sources(root), 'frontend-coverage-stale-source')
    equal_snapshot(report.get('runtimeSHA256'), frontend_runtime(root), 'frontend-coverage-stale-runtime')
    summary_path = regular(root, 'reports/desktop/coverage-summary.json')
    if report.get('coverageSummarySHA256') != sha256(summary_path):
        raise EvidenceError('frontend-coverage-raw-summary-mismatch')
    rows = frontend_rows(root, read_json(root, 'reports/desktop/coverage-summary.json'))
    if report.get('files') != rows or any(row['status'] != 'PASS' for row in rows):
        raise EvidenceError('frontend-coverage-floors-or-measurement-mismatch')


def go_rows(profile):
    totals = {}
    lines = profile.read_text().splitlines()
    if not lines or lines[0] != 'mode: atomic':
        raise EvidenceError('invalid-go-coverage-profile')
    seen = set()
    for line in lines[1:]:
        try:
            location, number, count = line.rsplit(' ', 2)
            number, count = int(number), int(count)
        except ValueError:
            raise EvidenceError('invalid-go-coverage-block') from None
        if number < 0 or count < 0 or location in seen:
            raise EvidenceError('invalid-go-coverage-block')
        seen.add(location)
        source = location.rsplit(':', 1)[0]
        if source.startswith('localwallet/tests/oracles/'):
            continue
        package = str(pathlib.PurePosixPath(source).parent)
        covered, total = totals.get(package, (0, 0))
        totals[package] = covered + (number if count else 0), total + number
    rows = []
    for package, (covered, total) in sorted(totals.items()):
        critical = any('/' + name in package for name in ('vault', 'derivation', 'signer', 'amount', 'chains', 'exchange', 'ipc', 'journal', 'qr', 'strictjson', 'isolation', 'secureui', 'uiprompt', 'supervisor', 'wallet-core', 'wallet-signer'))
        floor = 95 if critical else 90
        percent = 100 * covered / total if total else 100
        rows.append({'package': package, 'statements': total, 'covered': covered, 'percent': round(percent, 2), 'floor': floor,
                     'status': 'PASS' if percent >= floor else 'FAIL'})
    if not rows:
        raise EvidenceError('empty-go-coverage-profile')
    return rows


def validate_go(root, report, floor_report):
    if report.get('status') != 'PASS' or report.get('floors') != 'PASS':
        raise EvidenceError('go-coverage-missing-or-incomplete')
    equal_snapshot(report.get('sourceSHA256'), go_sources(root), 'go-coverage-stale-or-incomplete-source-set')
    gates = report.get('gates')
    if not isinstance(gates, list) or len(gates) != 3 or {row.get('gate') for row in gates if isinstance(row, dict)} != {'decred-simnet', 'ton-v5r1-emulator', 'usdt-evm-execution'} or any(not isinstance(row, dict) or row.get('status') != 'PASS' for row in gates):
        raise EvidenceError('go-coverage-original-local-gates-incomplete')
    profiles = report.get('profiles')
    required = {'reports/go-base.cover', 'reports/go-native.cover'}
    if not isinstance(profiles, list) or len(profiles) != 2 or any(not isinstance(row, dict) for row in profiles) or {row.get('file') for row in profiles} != required:
        raise EvidenceError('go-coverage-raw-profiles-incomplete')
    for row in profiles:
        if row.get('sha256') != sha256(regular(root, row['file'])):
            raise EvidenceError('go-coverage-raw-profile-mismatch')
    # Recreate the exact union rather than accepting a modified merged profile
    # alongside authentic input-profile hashes.
    blocks = {}
    sizes = {}
    for name in sorted(required):
        lines = regular(root, name).read_text().splitlines()
        if not lines or lines[0] != 'mode: atomic':
            raise EvidenceError('invalid-go-coverage-profile')
        for line in lines[1:]:
            try:
                location, number, count = line.rsplit(' ', 2)
                number, count = int(number), int(count)
            except ValueError:
                raise EvidenceError('invalid-go-coverage-block') from None
            source = location.rsplit(':', 1)[0]
            if source in ('localwallet/internal/secureui/automation.go', 'localwallet/internal/secureui/recovery_automation.go') or source.startswith('localwallet/tests/oracles/'):
                continue
            if number < 0 or count < 0 or (location in sizes and sizes[location] != number):
                raise EvidenceError('invalid-go-coverage-block')
            sizes[location] = number
            blocks[(location, number)] = max(blocks.get((location, number), 0), count)
    merged = 'mode: atomic\n' + ''.join(f'{location} {number} {count}\n' for (location, number), count in sorted(blocks.items()))
    profile = regular(root, 'reports/go.cover')
    if profile.read_text() != merged:
        raise EvidenceError('go-coverage-union-profile-mismatch')
    rows = go_rows(profile)
    if floor_report.get('packages') != rows or any(row['status'] != 'PASS' for row in rows):
        raise EvidenceError('go-coverage-floors-or-measurement-mismatch')
