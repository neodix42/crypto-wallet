#!/usr/bin/env bash
# Optional per-user desktop registration. The wallet itself does not need Python.
set -euo pipefail
if ! command -v python3 >/dev/null 2>&1; then
  echo 'Desktop registration needs python3: sudo apt install python3' >&2
  exit 1
fi
exec python3 - "$0" "$@" <<'PY'
import argparse
import grp
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import pwd

parser = argparse.ArgumentParser(description='Register CryptoWallet for the current user; never opens a wallet or changes wallet storage.')
parser.add_argument('--dry-run', action='store_true', help='show paths without writing anything')
parser.add_argument('--uninstall', action='store_true', help='remove this managed app/menu/icon installation; keep all wallet data')
desktop_options = parser.add_mutually_exclusive_group()
desktop_options.add_argument('--desktop', action='store_true', help='also create a desktop shortcut (create ~/Desktop if no desktop directory is configured)')
desktop_options.add_argument('--no-desktop', action='store_true', help='register the application menu only')
args = parser.parse_args(sys.argv[2:])

def require(condition, message):
    if not condition:
        raise ValueError(message)

def absolute(value, name):
    path = Path(value)
    require(path.is_absolute() and '\n' not in str(path) and '\r' not in str(path), name + ' must be an absolute path without newlines')
    return path

def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def safe_file(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == os.getuid() and
            not info.st_mode & 0o6022, 'Refusing unsafe or linked managed file: ' + str(path))

def safe_directories(path):
    # Reject links in the destination chain, including dangling links. Never
    # follow a launcher/icon link into an unrelated file during install/remove.
    username = pwd.getpwuid(os.getuid()).pw_name
    for directory in [*reversed(path.parents), path]:
        if directory.exists() or directory.is_symlink():
            info = directory.lstat()
            require(stat.S_ISDIR(info.st_mode), 'Refusing linked/non-directory destination: ' + str(directory))
            group_private = False
            if info.st_gid == os.getgid():
                group_private = set(grp.getgrgid(info.st_gid).gr_mem) <= {username} and all(
                    user.pw_uid == os.getuid() for user in pwd.getpwall() if user.pw_gid == info.st_gid)
            # Root-owned sticky temporary directories protect our owned child
            # from replacement by another user. A non-sticky writable ancestor
            # does not, even when the destination itself is private and owned.
            trusted_sticky = info.st_uid == 0 and bool(info.st_mode & stat.S_ISVTX)
            require(info.st_uid in (0, os.getuid()) and
                    (trusted_sticky or (not info.st_mode & 0o002 and
                     (not info.st_mode & 0o020 or group_private))),
                    'Unsafe destination ancestor ownership/permissions: ' + str(directory))
            if directory == path:
                require(info.st_uid == os.getuid(), 'Destination must be owned by this user: ' + str(path))

def entry(value):
    return value.replace('\\', '\\\\').replace('\n', '\\n').replace('\r', '\\r').replace('\t', '\\t')

def launcher(binary, icon):
    # Desktop Exec syntax is not shell syntax. Quote all reserved characters,
    # escape field-code percent signs, then apply desktop-entry string escaping.
    quoted = str(binary).replace('\\', '\\\\').replace('"', '\\"').replace('`', '\\`').replace('$', '\\$').replace('%', '%%')
    command = ('"/usr/bin/env" ' if '%' in str(binary) else '') + '"' + quoted + '"'
    return ('[Desktop Entry]\nType=Application\nName=CryptoWallet\nComment=Local multi-network cryptocurrency wallet\n'
            'Exec=' + entry(command) + '\nIcon=' + entry(str(icon)) + '\nTerminal=false\nCategories=Office;Finance;\n'
            'StartupWMClass=cryptowallet\n').encode()

def desktop_directory(home):
    configured = os.environ.get('XDG_DESKTOP_DIR')
    if not configured and shutil.which('xdg-user-dir'):
        result = subprocess.run(['xdg-user-dir', 'DESKTOP'], capture_output=True, text=True, timeout=5)
        configured = result.stdout.strip() if result.returncode == 0 else None
    candidate = absolute(configured, 'XDG_DESKTOP_DIR') if configured else home / 'Desktop'
    if candidate == home:
        # XDG uses HOME to mean desktop icons are disabled. Preserve that
        # default, but honor an explicit request by creating the conventional
        # separate Desktop directory rather than putting a launcher in HOME.
        return home / 'Desktop' if args.desktop else None
    if not args.desktop and not candidate.is_dir():
        return None
    return candidate

def atomic_copy(source, destination, mode):
    destination.parent.mkdir(parents=True, exist_ok=True)
    safe_directories(destination.parent)
    descriptor, temporary = tempfile.mkstemp(prefix='.cryptowallet-', dir=destination.parent)
    try:
        with os.fdopen(descriptor, 'wb') as output, source.open('rb') as incoming:
            shutil.copyfileobj(incoming, output)
            os.fchmod(output.fileno(), mode)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, destination)
    finally:
        Path(temporary).unlink(missing_ok=True)

def notify(command):
    # Desktop metadata support varies; the installed absolute-path menu entry
    # and icon remain usable even when a desktop service is absent/unavailable.
    try:
        subprocess.run(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10, check=False)
    except (OSError, subprocess.SubprocessError):
        print('Desktop metadata refresh unavailable; menu entry and icons are installed.')

def main():
    os.umask(0o077)
    require(os.getuid() != 0, 'Run this installer as an ordinary user, without sudo')
    home = absolute(os.environ.get('HOME', ''), 'HOME')
    data = absolute(os.environ.get('XDG_DATA_HOME') or str(home / '.local/share'), 'XDG_DATA_HOME')
    app = data / 'cryptowallet-app'
    marker = app / '.cryptowallet-user-install.json'
    safe_directories(data)
    safe_directories(app)
    old = None
    if marker.exists() or marker.is_symlink():
        safe_file(marker)
        require(marker.stat().st_size <= 32768, 'Oversized installation marker')
        old = json.loads(marker.read_text())
        require(old.get('schemaVersion') == 1 and isinstance(old.get('sha256'), dict), 'Invalid installation marker')
    shortcut_dir = None if args.no_desktop or args.uninstall else desktop_directory(home)
    # Preserve the previously registered shortcut across menu-only upgrades;
    # --no-desktop prevents creating a new one, rather than deleting user files.
    if old and old.get('desktopDirectory'):
        shortcut_dir = absolute(old['desktopDirectory'], 'Recorded desktop directory')
    targets = {
        'binary': app / 'cryptowallet', 'installer': app / 'install-user.sh',
        'png': app / 'cryptowallet.png', 'svg': app / 'cryptowallet.svg',
        'menu': data / 'applications/cryptowallet.desktop',
        'themePNG': data / 'icons/hicolor/512x512/apps/cryptowallet.png',
        'themeSVG': data / 'icons/hicolor/scalable/apps/cryptowallet.svg',
    }
    if shortcut_dir:
        require(shortcut_dir != home, 'Desktop shortcut directory cannot be the home directory')
        targets['shortcut'] = shortcut_dir / 'cryptowallet.desktop'
    if old:
        expected_old = set(targets) - {'shortcut'}
        if old.get('desktopDirectory'):
            expected_old.add('shortcut')
        require(set(old['sha256']) == expected_old, 'Installation inventory differs; use the same XDG_DATA_HOME to update/remove')
    for key, path in targets.items():
        safe_directories(path.parent)
        if path.exists() or path.is_symlink():
            safe_file(path)
            require(old is not None and key in old['sha256'] and digest(path) == old['sha256'][key],
                    'Preserved an unrelated or modified file: ' + str(path))
    if args.uninstall:
        if not old:
            print('No managed CryptoWallet desktop installation found. Wallet data was not touched.')
            return
        print('Remove managed app, menu entry and icons from ' + str(data) + '; preserve all wallet data.')
        if args.dry_run:
            return
        for path in targets.values():
            path.unlink(missing_ok=True)
        marker.unlink()
        try:
            app.rmdir()  # Unrelated files inside the app directory are retained.
        except OSError:
            pass
    else:
        source = Path(sys.argv[1]).resolve().parent
        if not (source / 'cryptowallet').is_file():
            source = source.parent / 'bin'
            icons = source.parent / 'internal/branding'
            script = Path(sys.argv[1]).resolve()
        else:
            icons, script = source, source / 'install-user.sh'
        sources = {'binary': source / 'cryptowallet', 'installer': script,
                   'png': icons / 'cryptowallet.png', 'svg': icons / 'cryptowallet.svg'}
        for path in sources.values():
            require(path.is_file() and not path.is_symlink(), 'Missing/linked release file: ' + str(path))
        binary_info = sources['binary'].stat()
        require(binary_info.st_mode & 0o111 and not binary_info.st_mode & 0o6000, 'Release executable has unsafe permissions')
        with sources['binary'].open('rb') as stream:
            require(stream.read(7) == b'\x7fELF\x02\x01\x01', 'Release executable is not a Linux ELF64 binary')
        print('App: ' + str(app / 'cryptowallet'))
        print('Application menu: ' + str(targets['menu']))
        if shortcut_dir:
            print('Desktop shortcut: ' + str(targets['shortcut']))
        print('Wallet storage is preserved. The application is not launched.')
        if args.dry_run:
            return
        data.mkdir(parents=True, exist_ok=True)
        safe_directories(data)
        with tempfile.TemporaryDirectory(prefix='.cryptowallet-install-', dir=data) as temporary:
            staging = Path(temporary)
            for key, path in sources.items():
                shutil.copyfile(path, staging / key)
            (staging / 'menu').write_bytes(launcher(targets['binary'], targets['png']))
            shutil.copyfile(staging / 'png', staging / 'themePNG')
            shutil.copyfile(staging / 'svg', staging / 'themeSVG')
            if shortcut_dir:
                shutil.copyfile(staging / 'menu', staging / 'shortcut')
            record = {'schemaVersion': 1, 'desktopDirectory': str(shortcut_dir) if shortcut_dir else None,
                      'sha256': {key: digest(staging / key) for key in targets}}
            (staging / 'marker').write_text(json.dumps(record, indent=2) + '\n')
            operations = {**targets, 'marker': marker}
            completed = []
            try:
                for key, path in operations.items():
                    previous = staging / ('previous-' + key)
                    if path.exists():
                        shutil.copy2(path, previous)
                    completed.append((path, previous))
                    atomic_copy(staging / key, path, 0o600 if key == 'marker' else
                                0o755 if key in ('binary', 'installer', 'shortcut') else 0o644)
            except BaseException:
                for path, previous in reversed(completed):
                    if previous.exists():
                        atomic_copy(previous, path, stat.S_IMODE(previous.stat().st_mode))
                    else:
                        path.unlink(missing_ok=True)
                raise
        if shortcut_dir and shutil.which('gio'):
            notify(['gio', 'set', '--type', 'string', str(targets['shortcut']), 'metadata::trusted', 'true'])
        print('Installed. Open CryptoWallet from your application menu.')
        if shortcut_dir:
            print('Your desktop may ask you to allow launching the shortcut.')
        print('To remove menu/icons/app only: ' + str(app / 'install-user.sh') + ' --uninstall')
    if shutil.which('update-desktop-database') and (data / 'applications').is_dir():
        notify(['update-desktop-database', str(data / 'applications')])

try:
    main()
except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
    raise SystemExit('Desktop installation failed: ' + str(error))
PY
