#!/usr/bin/env python3
"""Publish only the existing Go components. No Node, Electron or Debian package.
These are real protocol/signing components, not a standalone human wallet UI.
"""
import hashlib,json,pathlib,shutil,subprocess,tarfile,tempfile
from version import selected_version
root=pathlib.Path(__file__).resolve().parents[1]
names=('wallet-core','wallet-signer','wallet-launcher')
inventories=('go-sbom.cdx.json','go-dependency-licenses.json','go-source-manifest.json','go-THIRD-PARTY-NOTICES.txt','go-THIRD-PARTY-NOTICES.json')
def digest(p):
 with pathlib.Path(p).open('rb') as stream:return hashlib.file_digest(stream,'sha256').hexdigest()
def package():
 version=selected_version(root);bins=root/'bin';dist=root/'dist';dist.mkdir(exist_ok=True)
 subprocess.run(['python3','scripts/license-notices.py','--go-only'],cwd=root,check=True)
 metadata={}
 for name in inventories:
  entry=dist/name
  if entry.is_symlink() or not entry.is_file() or entry.stat().st_size>64*1024*1024:raise RuntimeError('Missing or unsafe release inventory: '+name)
  if name.endswith('.json'):json.loads(entry.read_text())
  metadata[name]=digest(entry)
 binary_manifest=json.loads((bins/'manifest.json').read_text())
 if set(binary_manifest)!=set(names):raise RuntimeError('Unexpected Go component manifest')
 for name in names:
  binary=bins/name
  if binary.is_symlink() or not binary.is_file() or digest(binary)!=binary_manifest[name]:raise RuntimeError('Go component identity mismatch: '+name)
  if subprocess.check_output([str(binary),'--version'],text=True,timeout=10)!=version+'\n':raise RuntimeError('Go component version mismatch: '+name)
 manifest={'schemaVersion':1,'kind':'go-components','version':version,'status':'DEVELOPMENT_UNSIGNED','source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'dirty':bool(subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()),'go':subprocess.check_output(['go','version'],text=True).strip(),'platform':'linux-amd64','binaryManifest':binary_manifest,'inventorySHA256':metadata}
 dirname='localwallet-go-'+version+'-linux-amd64';archive=dist/(dirname+'.tar.gz')
 with tempfile.TemporaryDirectory(prefix='wallet-go-artifact-') as tmp:
  folder=pathlib.Path(tmp)/dirname;folder.mkdir()
  for name in names:shutil.copy2(bins/name,folder/name)
  for name in inventories:shutil.copy2(dist/name,folder/name)
  (folder/'manifest.json').write_text(json.dumps(binary_manifest,sort_keys=True)+'\n')
  (folder/'go-build-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
  (folder/'README.md').write_text('''# Go wallet components

These executables do not depend on .tools, Go, Node, npm, Python or the source repository at runtime.
They are backend components of the existing Go + Electron wallet; this archive has no standalone main wallet interface.

wallet-core is the public coordinator using bounded framed stdin/stdout IPC.
wallet-signer is the separate isolated signer producing typed Electron prompt requests.
It opens no Go/GTK windows; user-facing input requires the complete Electron desktop.
Keep wallet-core, wallet-signer and manifest.json together. Do not enter credentials into a terminal or protocol client.
wallet-launcher belongs to the optional complete Electron desktop layout; it cannot start a desktop from this flat archive.

Ubuntu amd64 Go runtime libraries: libc and libseccomp2.
The separate complete Electron desktop needs its normal graphical runtime libraries and X11/Wayland session. Blockchain node/test tools are not needed.
Run each executable with --version to inspect its public version without opening a wallet.

This unsigned development build has incomplete production release gates. No mainnet spending is automated.
''')
  temporary=dist/(archive.name+'.tmp')
  try:
   with tarfile.open(temporary,'w:gz') as tar:tar.add(folder,arcname=dirname)
   temporary.replace(archive)
  finally:
   temporary.unlink(missing_ok=True)
 manifest['archive']={'file':archive.name,'sha256':digest(archive)}
 for name in names:shutil.copy2(bins/name,dist/name)
 shutil.copy2(bins/'manifest.json',dist/'manifest.json')
 for parent in [bins,dist]:(parent/'go-build-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
 (dist/'GO-SHA256SUMS').write_text(manifest['archive']['sha256']+'  '+archive.name+'\n'+''.join(binary_manifest[name]+'  '+name+'\n' for name in names)+''.join(metadata[name]+'  '+name+'\n' for name in inventories))
 print('Go components: '+str(archive.relative_to(root)))
 print('No .tools or build toolchain is required at runtime; the existing desktop interface is separate.')
if __name__=='__main__':package()
