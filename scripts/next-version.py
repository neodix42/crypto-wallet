#!/usr/bin/env python3
"""Increment the greatest bounded canonical tag/base patch; never create a tag."""
import pathlib
import subprocess
from version import MAX_COMPONENT, validate_version

root = pathlib.Path(__file__).resolve().parents[1]


def next_version(base, tags):
    validate_version(base)
    versions = [tuple(map(int, base.split('.')))]
    for tag in tags:
        if not isinstance(tag, str) or not tag.startswith('v'):
            continue
        try:
            value = validate_version(tag[1:])
        except ValueError:
            continue
        versions.append(tuple(map(int, value.split('.'))))
    major, minor, patch = max(versions)
    if patch >= MAX_COMPONENT:
        raise ValueError('Version patch exhausted; update major/minor explicitly')
    return f'{major}.{minor}.{patch + 1}'


if __name__ == '__main__':
    try:
        tags = subprocess.check_output(['git', 'tag', '--list'], cwd=root, text=True).splitlines()
        print(next_version((root / 'VERSION').read_text().strip(), tags))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error))
