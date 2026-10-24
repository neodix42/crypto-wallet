#!/usr/bin/env python3
"""Developer/CI-only pinned GitHub CLI; never a wallet runtime dependency."""
import hashlib
import pathlib
import shutil
import tarfile
import tempfile
import urllib.request

root = pathlib.Path(__file__).resolve().parents[1]
version = '2.103.2'
checksum = '62ccf747115ddc61237efffdde66a3c8f70ba249433dc64f70801089b59c59c8'
cache = root / '.tools'
target = cache / ('gh-' + version)
if not (target / 'bin/gh').is_file():
    cache.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='wallet-gh-download-', dir=cache) as temporary:
        temporary = pathlib.Path(temporary)
        archive = temporary / 'gh.tar.gz'
        url = f'https://github.com/cli/cli/releases/download/v{version}/gh_{version}_linux_amd64.tar.gz'
        with urllib.request.urlopen(url, timeout=60) as source, archive.open('wb') as output:
            shutil.copyfileobj(source, output)
        if hashlib.sha256(archive.read_bytes()).hexdigest() != checksum:
            raise SystemExit('GitHub CLI archive SHA256 mismatch')
        with tarfile.open(archive, 'r:gz') as stream:
            stream.extractall(temporary, filter='data')
        shutil.move(temporary / f'gh_{version}_linux_amd64', target)
print(target / 'bin')
