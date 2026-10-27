#!/usr/bin/env python3
"""Increment numeric release history (X.Y.Z or legacy vX.Y.Z); never create a tag."""
import pathlib
import subprocess
from version import MAX_COMPONENT, validate_version

root = pathlib.Path(__file__).resolve().parents[1]


def next_version(base, tags):
    validate_version(base)
    # Removing the v prefix changes presentation, not release history. Both
    # formats consume the same numeric version; new publications use bare tags.
    minimum = tuple(map(int, base.split('.')))
    versions = []
    for tag in tags:
        value = tag[1:] if isinstance(tag, str) and tag.startswith('v') else tag
        try:
            value = validate_version(value)
        except (ValueError, TypeError):
            continue
        versions.append(tuple(map(int, value.split('.'))))
    if not versions or max(versions) < minimum:
        return base
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
