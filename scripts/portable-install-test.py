#!/usr/bin/env python3
"""Exercise the actual built executable's installation in disposable directories.

Only diagnostic executable switches run. No real profile or repository-root
installed file is opened or replaced by this drill.
"""
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import stat
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('portable_install', ROOT / 'scripts/install.py')
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def install(source, destination):
    with contextlib.redirect_stdout(io.StringIO()):
        installer.install_portable(source, destination)


def expect_refusal(source, destination):
    try:
        install(source, destination)
    except RuntimeError:
        return
    raise RuntimeError('Unrelated or changed executable was overwritten')


def main():
    source = ROOT / 'bin/cryptowallet'
    expected = digest(source)
    report = dict(status='RUNNING', scope=__doc__, artifact=dict(sha256=expected), checks=[],
                  producerSHA256=digest(Path(__file__)),
                  sourceSHA256={name: digest(ROOT / name) for name in
                                ('scripts/install.py', 'scripts/package-portable.py', 'scripts/version.py')},
                  notRun=['Real profile migration', 'Clean installed OS matrix', 'Repository-root install'])
    try:
        with tempfile.TemporaryDirectory(prefix='wallet-actual-install-') as temporary:
            parent = Path(temporary)
            def destination(name):
                path = parent / name
                path.mkdir(mode=0o700)
                return path
            fresh = destination('fresh')
            sentinel = fresh / 'public-profile-sentinel'
            sentinel.write_bytes(b'public disposable profile preservation fixture')
            install(source, fresh)
            target, marker = fresh / 'cryptowallet', fresh / '.cryptowallet-install.json'
            require(not target.is_symlink() and digest(target) == expected and
                    stat.S_IMODE(target.stat().st_mode) == 0o755 and
                    stat.S_IMODE(marker.stat().st_mode) == 0o600,
                    'Installed file/hash/permissions differ')
            report['checks'].append('Fresh actual executable installs as regular 0755 with managed 0600 marker')
            install(source, fresh)
            require(digest(target) == expected and sentinel.read_bytes() == b'public disposable profile preservation fixture',
                    'Managed repeat install changed the candidate or profile sentinel')
            report['checks'].append('Managed repeat install preserves executable identity and public profile sentinel')
            legacy = destination('legacy')
            runtime = legacy / 'wallet-runtime'
            runtime.mkdir(mode=0o700)
            (runtime / '.localwallet-managed').touch(mode=0o600)
            generation = runtime / ('app-' + 'a' * 32)
            generation.mkdir(mode=0o700)
            previous = generation / 'cryptowallet'
            previous.write_bytes(b'public managed legacy launcher fixture')
            (legacy / 'cryptowallet').symlink_to(Path('wallet-runtime') / generation.name / 'cryptowallet')
            install(source, legacy)
            require(not (legacy / 'cryptowallet').is_symlink() and
                    digest(legacy / 'cryptowallet') == expected and
                    previous.read_bytes() == b'public managed legacy launcher fixture',
                    'Managed legacy replacement damaged the previous runtime')
            report['checks'].append('Managed legacy link upgrades to actual executable without deleting its runtime')
            unrelated = destination('unrelated-file')
            (unrelated / 'cryptowallet').write_bytes(b'public unrelated user file')
            expect_refusal(source, unrelated)
            require((unrelated / 'cryptowallet').read_bytes() == b'public unrelated user file',
                    'Unrelated file changed')
            report['checks'].append('Unrelated regular file is refused and preserved')
            linked = destination('unrelated-link')
            (linked / 'cryptowallet').symlink_to('unrelated-public-file')
            expect_refusal(source, linked)
            require((linked / 'cryptowallet').is_symlink() and
                    (linked / 'cryptowallet').readlink() == Path('unrelated-public-file'),
                    'Unrelated symlink changed')
            report['checks'].append('Unrelated dangling link is refused and preserved')
            with target.open('ab') as output:
                output.write(b'public-tamper-fixture')
            changed = digest(target)
            expect_refusal(source, fresh)
            require(digest(target) == changed, 'Modified managed file changed after refusal')
            report['checks'].append('Modified previously managed executable is refused without execution or replacement')
        require(digest(source) == expected, 'Built candidate changed during the install drill')
        report['status'] = 'PASS'
    except Exception:
        report.update(status='FAIL', reason='Actual temporary installation failed; no profile data retained')
    (ROOT / 'reports').mkdir(exist_ok=True)
    (ROOT / 'reports/portable-install.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
