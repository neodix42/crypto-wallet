#!/usr/bin/env python3
"""Actual Wails Go build; permit frontend compilation, prohibit Electron/Debian tools."""
import hashlib
import json
import os
import pathlib
import shlex
import subprocess
import tarfile
import tempfile
import time

root = pathlib.Path(__file__).resolve().parents[1]
reports = root / 'reports'
reports.mkdir(exist_ok=True)


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def debian_artifacts():
    return {str(path.relative_to(root)): [path.stat().st_size, path.stat().st_mtime_ns]
            for path in (root / 'dist').glob('*.deb')}


started = time.monotonic()
before = debian_artifacts()
with tempfile.TemporaryDirectory(prefix='wallet-go-only-proof-') as temporary:
    folder = pathlib.Path(temporary)
    marker = folder / 'unexpected-tool'
    for name in ('electron', 'dpkg-deb'):
        executable = folder / name
        executable.write_text('#!/bin/sh\nprintf "%s\\n" ' + shlex.quote(name) + ' >> ' + shlex.quote(str(marker)) + '\nexit 97\n')
        executable.chmod(0o755)
    env = {**os.environ, 'PATH': str(folder) + ':' + os.environ['PATH']}
    result = subprocess.run(['make', 'package-go'], cwd=root, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=600)
    (reports / 'go-only-build.log').write_text(result.stdout)
    unexpected = marker.read_text().splitlines() if marker.exists() else []
    checks = {}
    reason = None
    try:
        if result.returncode != 0:
            raise ValueError('Go build/package command failed')
        manifest = json.loads((root / 'bin/portable-build-manifest.json').read_text())
        version = manifest['version']
        archive = root / 'dist' / f'cryptowallet-{version}-linux-amd64.tar.gz'
        extracted = folder / 'downloaded'
        extracted.mkdir()
        with tarfile.open(archive) as package:
            members = package.getmembers()
            if len(members) != 1 or members[0].name != 'cryptowallet' or not members[0].isfile() or members[0].mode != 0o755:
                raise ValueError('Archive must contain only one regular cryptowallet executable')
            package.extractall(extracted, members=members, filter='data')
        binary = extracted / 'cryptowallet'
        isolated_env = {'PATH': '/usr/bin:/bin', 'HOME': str(folder / 'empty-home'),
                        'XDG_CONFIG_HOME': str(folder / 'empty-config')}
        runtime_version = subprocess.check_output([str(binary), '--version'], cwd=extracted,
                                                  env=isolated_env, text=True, timeout=15)
        runtime_info = subprocess.check_output([str(binary), '--runtime-info'], cwd=extracted,
                                               env=isolated_env, text=True, timeout=15)
        licenses = subprocess.check_output([str(binary), '--licenses'], cwd=extracted,
                                           env=isolated_env, timeout=15)
        if runtime_version != version + '\n' or runtime_info != 'wails-webkitgtk4.1; portable-go; sandbox=none\n':
            raise ValueError('Extracted version/native runtime differs')
        if b'CryptoWallet third-party redistribution texts' not in licenses or b'go-toolchain:' not in licenses or b'go:' not in licenses:
            raise ValueError('Extracted binary lacks embedded notices')
        if digest(binary) != manifest['sha256']:
            raise ValueError('Extracted executable hash differs from built candidate')
        checks = {'versionWithoutCacheOrToolchain': version, 'runtimeInfo': runtime_info.strip(),
                  'sha256': digest(binary), 'archive': archive.name,
                  'singleRegularExecutable': True, 'embeddedNoticeBytes': len(licenses),
                  'privateProfileCreated': (folder / 'empty-config').exists()}
        if checks['privateProfileCreated']:
            raise ValueError('Inspection unexpectedly opened a profile')
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        reason = str(error)
    report = {'status': 'PASS' if reason is None and not unexpected and before == debian_artifacts() else 'FAIL',
              'command': ['make', 'package-go'], 'exit': result.returncode,
              'checks': checks, 'forbiddenToolsInvoked': unexpected,
              'debianArtifactsUnchanged': before == debian_artifacts(),
              'seconds': round(time.monotonic() - started, 3),
              'scope': 'Actual compiled Wails Go executable/one-file archive, with embedded React frontend, inspected from isolated extraction without developer cache or toolchain; Node/npm build-time only; GUI/recovery/blockchain acceptance is separate',
              'notRun': ['clean Ubuntu OS/display matrix', 'live funded testnets', 'independent security/license review']}
    if reason:
        report['reason'] = reason
    (reports / 'go-only-build.json').write_text(json.dumps(report, indent=2) + '\n')
    print(result.stdout)
    print(json.dumps(report, indent=2))
    raise SystemExit(report['status'] != 'PASS')
