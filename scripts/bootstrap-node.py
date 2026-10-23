#!/usr/bin/env python3
"""Pinned official Node archive, independently frozen SHA256; no system writes."""
import hashlib, pathlib, shutil, tarfile, tempfile, urllib.request
root=pathlib.Path(__file__).resolve().parents[1]
name='node-v24.19.0-linux-x64'
expected='14b342e71204f811bde6153be8e04b62aef63c236fef92b55f9c83154b409647'
tools=root/'.tools';tools.mkdir(exist_ok=True)
with tempfile.TemporaryDirectory(dir=tools,prefix='node-bootstrap-') as temporary:
 folder=pathlib.Path(temporary);archive=folder/'node.tar.xz'
 with urllib.request.urlopen('https://nodejs.org/dist/v24.19.0/'+name+'.tar.xz',timeout=60) as response,archive.open('wb') as target:
  shutil.copyfileobj(response,target)
 if hashlib.sha256(archive.read_bytes()).hexdigest()!=expected:raise SystemExit('Pinned Node archive SHA256 mismatch; refusing to execute it')
 with tarfile.open(archive) as tar:
  # The pinned official archive has relative npm/node symlinks. Reject traversal
  # and device entries independently of the checksum before extraction.
  for member in tar.getmembers():
   p=pathlib.PurePosixPath(member.name)
   if p.is_absolute() or '..' in p.parts or not p.parts or p.parts[0]!=name or member.isdev():raise SystemExit('Unsafe Node archive entry')
  tar.extractall(folder,filter='data')
 destination=tools/name
 if destination.exists():raise SystemExit('Node cache destination appeared concurrently; retry build')
 (folder/name).rename(destination)
print('Verified pinned Node runtime installed in '+str(destination.relative_to(root)))
