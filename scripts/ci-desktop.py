#!/usr/bin/env python3
"""Permit isolated desktop tests in a named, ephemeral GitHub-hosted CI profile.

Ubuntu 24.04 restricts user namespaces for otherwise unconfined processes.
The named AppArmor profile grants that permission only to commands explicitly
started with aa-exec. It has no executable attachment, persistent policy file,
setuid helper change or system-wide sysctl change. Wails uses namespaces only
for its owned test infrastructure, not application sandboxing. Retained legacy
Electron/launcher sandbox protections remain enabled in their explicit tests.
"""
import argparse
import hashlib
import os
import pathlib
import platform
import select
import subprocess
import sys
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]
SUDO = '/usr/bin/sudo'
PARSER = '/usr/sbin/apparmor_parser'
AA_EXEC = '/usr/bin/aa-exec'
PROFILES = pathlib.Path('/sys/kernel/security/apparmor/profiles')
ENABLED = pathlib.Path('/sys/module/apparmor/parameters/enabled')
PROFILE_LIMIT = 1048576
PROFILE_TIMEOUT = 10
COMMANDS = frozenset({
    ('make', 'coverage'),
    ('make', 'test-packaged'),
    ('make', 'test-wails-wallet'),
    ('make', 'test-send-portable'),
    ('./scripts/with-toolchain.sh', 'python3', 'scripts/wails-wallet-test.py',
     '--ton-only', '--report-name', 'wails-ton-deployment'),
    ('make', 'test-packaged-desktop'),
    ('make', 'test-packaged-send-desktop'),
    ('./scripts/with-toolchain.sh', 'python3', 'scripts/packaged-send.py'),
    ('./scripts/with-toolchain.sh', 'python3', 'scripts/display-startup.py', '--backend', 'x11'),
})


def require_ci():
    """Never load or execute a relaxed profile on a developer/user machine."""
    if (os.environ.get('GITHUB_ACTIONS') != 'true'
            or os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted'
            or platform.system() != 'Linux' or os.geteuid() == 0):
        raise RuntimeError('This helper requires a non-root GitHub-hosted Linux Actions runner')
    raw = os.environ.get('GITHUB_WORKSPACE', '')
    workspace = pathlib.Path(raw)
    if (not workspace.is_absolute() or workspace.resolve(strict=True) != ROOT
            or pathlib.Path.cwd().resolve() != ROOT
            or ROOT.stat().st_uid != os.geteuid()):
        raise RuntimeError('GitHub workspace must match the owned repository and working directory')
    if ENABLED.read_text().strip() != 'Y':
        raise RuntimeError('AppArmor is unavailable; desktop sandbox prerequisites are not satisfied')


def profile_name():
    suffix = hashlib.sha256(os.fsencode(str(ROOT))).hexdigest()[:24]
    return 'localwallet-ci-desktop-' + suffix


def policy_text():
    # Deliberately no attachment path or broad matching glob. Ordinary binaries
    # and processes receive no new permission without an explicit aa-exec.
    return 'abi <abi/4.0>,\nprofile ' + profile_name() + ' flags=(unconfined) {\n  userns,\n}\n'


def read_profiles():
    # securityfs can deny non-root reads despite displaying mode 0444. Read only
    # this fixed public kernel inventory; never a renderer/user-supplied path.
    process = subprocess.Popen([SUDO, '-n', '/usr/bin/cat', str(PROFILES)],
                               cwd=ROOT, stdout=subprocess.PIPE,
                               stderr=subprocess.DEVNULL)
    deadline = time.monotonic() + PROFILE_TIMEOUT
    output = bytearray()
    try:
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([process.stdout], [], [], remaining)[0]:
                raise RuntimeError('CI AppArmor profile lookup timed out')
            chunk = os.read(process.stdout.fileno(), min(65536, PROFILE_LIMIT + 1 - len(output)))
            if not chunk:
                break
            output.extend(chunk)
            if len(output) > PROFILE_LIMIT:
                raise RuntimeError('CI AppArmor profile inventory exceeds its bound')
        if process.wait(timeout=max(.001, deadline - time.monotonic())) != 0:
            raise RuntimeError('CI AppArmor profile inventory could not be read')
        try:
            return output.decode('utf-8')
        except UnicodeDecodeError:
            raise RuntimeError('Invalid CI AppArmor profile inventory') from None
    finally:
        process.stdout.close()
        if process.poll() is None:
            process.kill()
            process.wait(timeout=5)


def loaded():
    prefix = profile_name() + ' ('
    modes = [line for line in read_profiles().splitlines() if line.startswith(prefix)]
    if not modes:
        return False
    if modes != [profile_name() + ' (unconfined)']:
        raise RuntimeError('Unexpected AppArmor mode for this CI workspace profile')
    return True


def parser_command(action):
    with tempfile.TemporaryDirectory(prefix='localwallet-ci-apparmor-') as temporary:
        policy = pathlib.Path(temporary) / 'profile'
        descriptor = os.open(policy, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'w') as output:
            output.write(policy_text())
        # -K forbids loading or writing system profile caches. The only host
        # policy mutation is adding/removing this exact named kernel profile.
        subprocess.run([SUDO, '-n', PARSER, action, '-K', str(policy)],
                       cwd=ROOT, check=True)


def setup():
    require_ci()
    if not loaded():
        parser_command('-r')
    if not loaded():
        raise RuntimeError('CI desktop AppArmor profile was not loaded')
    print('CI desktop AppArmor profile ready: ' + profile_name())


def run(command):
    require_ci()
    if tuple(command) not in COMMANDS:
        raise RuntimeError('Only explicit packaged desktop and coverage checks are permitted by this helper')
    if not loaded():
        raise RuntimeError('CI desktop AppArmor profile is missing; run setup first')
    # aa-exec fails if it cannot select this profile. There is no unconfined or
    # no-sandbox fallback. The actual check and every descendant retain it.
    return subprocess.run([AA_EXEC, '-p', profile_name(), '--', *command],
                          cwd=ROOT, check=False).returncode


def cleanup():
    require_ci()
    if loaded():
        parser_command('-R')
    if loaded():
        raise RuntimeError('CI desktop AppArmor profile was not removed')
    print('CI desktop AppArmor profile removed: ' + profile_name())


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest='action', required=True)
    subparsers.add_parser('setup')
    execute = subparsers.add_parser('run')
    execute.add_argument('command', nargs=argparse.REMAINDER)
    subparsers.add_parser('cleanup')
    args = parser.parse_args(argv)
    try:
        if args.action == 'setup':
            setup()
        elif args.action == 'cleanup':
            cleanup()
        else:
            command = args.command[1:] if args.command[:1] == ['--'] else args.command
            return run(command)
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print('CI desktop sandbox prerequisite failed: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
