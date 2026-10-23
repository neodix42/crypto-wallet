#!/usr/bin/env python3
"""One bounded canonical version for Go, Electron, Debian and release artifacts."""
import os
import pathlib
import re

root = pathlib.Path(__file__).resolve().parents[1]
MAX_COMPONENT = 2147483647
_PATTERN = re.compile(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)')


def validate_version(value):
    if not isinstance(value, str) or not _PATTERN.fullmatch(value):
        raise ValueError('WALLET_VERSION/VERSION must be a canonical major.minor.patch version')
    parts = value.split('.')
    if any(len(part) > 10 or int(part) > MAX_COMPONENT for part in parts):
        raise ValueError('Version component too large')
    return value


def selected_version(base=root):
    value = os.environ.get('WALLET_VERSION')
    if value is None:
        value = (pathlib.Path(base) / 'VERSION').read_text().strip()
    return validate_version(value)


if __name__ == '__main__':
    try:
        print(selected_version())
    except (ValueError, OSError) as error:
        raise SystemExit(str(error))
