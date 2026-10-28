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
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
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


def desktop_install_drill(parent, source, expected, report):
    """Run the shipped installer against the actual Go file, diagnostics only."""
    version = subprocess.check_output([str(source), '--version'], text=True, timeout=15).strip()
    archive = ROOT / 'dist' / f'cryptowallet-{version}-linux-amd64.tar.gz'
    package_spec = importlib.util.spec_from_file_location('actual_desktop_package', ROOT / 'scripts/package-portable.py')
    package = importlib.util.module_from_spec(package_spec)
    package_spec.loader.exec_module(package)
    package.verify_archive(archive, source, version)
    import tarfile
    extraction = parent / 'user-desktop-extraction'
    extraction.mkdir(mode=0o700)
    with tarfile.open(archive) as portable:
        portable.extractall(extraction, filter='data')
    report['desktopArchive'] = {'file': archive.name, 'sha256': digest(archive),
                                'members': list(package.payloads(ROOT, source))}
    home = parent / 'desktop home space " quote $ dollar ` tick % percent'
    home.mkdir(mode=0o700)
    desktop = home / 'Desktop'
    data = home / 'data'
    profile = home / '.config/cryptowallet/mainnet'
    profile.mkdir(parents=True, mode=0o700)
    sentinel = profile / 'public-profile-preservation-fixture'
    sentinel.write_bytes(b'PUBLIC DISPOSABLE PRESERVATION FIXTURE; no wallet secrets')
    env = {**os.environ, 'HOME': str(home), 'XDG_DATA_HOME': str(data), 'XDG_DESKTOP_DIR': str(desktop),
           'XDG_CONFIG_HOME': str(home / '.config'), 'XDG_CACHE_HOME': str(home / '.cache')}
    for name in ('DISPLAY', 'WAYLAND_DISPLAY', 'DBUS_SESSION_BUS_ADDRESS'):
        env.pop(name, None)
    require(subprocess.check_output([str(extraction / 'cryptowallet'), '--version'], env=env,
                                    text=True, timeout=15) == version + '\n', 'Extracted Go version differs')
    subprocess.run([str(extraction / 'install-user.sh'), '--dry-run', '--desktop'], env=env,
                   check=True, capture_output=True, text=True, timeout=30)
    require(not data.exists(), 'Desktop installer dry-run wrote application data')
    for _repeat in range(2):
        subprocess.run([str(extraction / 'install-user.sh'), '--desktop'], env=env,
                       check=True, capture_output=True, text=True, timeout=30)
    app = data / 'cryptowallet-app'
    require(digest(app / 'cryptowallet') == expected and not (app / 'cryptowallet').is_symlink(),
            'Per-user installation differs from the actual Go executable')
    menu = data / 'applications/cryptowallet.desktop'
    require(digest(menu) == digest(desktop / 'cryptowallet.desktop'), 'Desktop and application menu entries differ')
    for name in ('cryptowallet.png', 'cryptowallet.svg'):
        require(digest(app / name) == digest(extraction / name), 'Installed desktop icon differs')
    require(digest(data / 'icons/hicolor/512x512/apps/cryptowallet.png') == digest(app / 'cryptowallet.png') and
            digest(data / 'icons/hicolor/scalable/apps/cryptowallet.svg') == digest(app / 'cryptowallet.svg'),
            'Installed icon-theme resources differ')
    before = {path: digest(path) for path in data.rglob('*') if path.is_file()}
    program = (extraction / 'install-user.sh').read_text().split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0]
    harness = ('import os\nreal_replace=os.replace\nfailed=False\n'
               'def fail_once(source,destination):\n'
               ' global failed\n'
               ' if str(destination).endswith("applications/cryptowallet.desktop") and not failed:\n'
               '  failed=True\n  raise OSError("PUBLIC FIXTURE atomic switch failure")\n'
               ' return real_replace(source,destination)\n'
               'os.replace=fail_once\n' + program)
    failed = subprocess.run([sys.executable, '-c', harness, str(extraction / 'install-user.sh')],
                            env=env, capture_output=True, text=True, timeout=30)
    require(failed.returncode != 0 and 'PUBLIC FIXTURE atomic switch failure' in failed.stderr and
            {path: digest(path) for path in data.rglob('*') if path.is_file()} == before,
            'Injected installer rename failure did not roll back the actual managed files')
    subprocess.run([str(extraction / 'install-user.sh')], env=env,
                   check=True, capture_output=True, text=True, timeout=30)
    if shutil.which('desktop-file-validate'):
        subprocess.run(['desktop-file-validate', str(menu)], check=True, capture_output=True, timeout=15)
    # Parse the real installed Exec through GIO using a disposable copy which
    # adds --version. This cannot open the GUI or wallet, even on a real display.
    diagnostic = parent / 'public-go-version.desktop'
    diagnostic.write_text('\n'.join(line + ' --version' if line.startswith('Exec=') else line
                                    for line in menu.read_text().splitlines()) + '\n')
    result = subprocess.run(['gio', 'launch', str(diagnostic)], env=env, check=True,
                            capture_output=True, text=True, timeout=15)
    require(result.stdout.strip() == version, 'GIO did not execute the exact installed Go version')
    report['checks'].append('Actual archive extracts exactly executable/installer/PNG/SVG with matching hashes/modes; dry-run writes nothing')
    report['checks'].append('Actual Go executable installs twice into isolated HOME/XDG and GIO correctly parses special-character paths for --version only')
    report['checks'].append('Injected atomic-switch failure rolls back every actual managed byte and a normal retry succeeds')
    shutil.rmtree(extraction)
    require(subprocess.check_output([str(app / 'cryptowallet'), '--version'], env=env, text=True, timeout=15) == version + '\n',
            'Installed Go executable depends on the extracted folder')
    subprocess.run([str(app / 'install-user.sh'), '--uninstall'], env=env,
                   check=True, capture_output=True, text=True, timeout=30)
    require(not (app / 'cryptowallet').exists() and not menu.exists() and
            not (desktop / 'cryptowallet.desktop').exists() and
            sentinel.read_bytes() == b'PUBLIC DISPOSABLE PRESERVATION FIXTURE; no wallet secrets' and
            list(profile.iterdir()) == [sentinel], 'Desktop uninstall changed the disposable profile or retained managed app/menu')
    report['checks'].append('Moved-download independence and per-user uninstall preserve the disposable profile byte-for-byte; no GUI or real profile opened')


def main():
    source = ROOT / 'bin/cryptowallet'
    expected = digest(source)
    report = dict(status='RUNNING', scope=__doc__, artifact=dict(sha256=expected), checks=[],
                  producerSHA256=digest(Path(__file__)),
                  sourceSHA256={name: digest(ROOT / name) for name in
                                ('scripts/install.py', 'scripts/install-user.sh', 'scripts/package-portable.py', 'scripts/version.py',
                                 'internal/branding/cryptowallet.png', 'internal/branding/cryptowallet.svg')},
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
            desktop_install_drill(parent, source, expected, report)
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
