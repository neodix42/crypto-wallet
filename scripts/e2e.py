#!/usr/bin/env python3
"""Portable desktop runtime startup in disposable containers. This is explicitly
not the clean desktop X11+Wayland acceptance matrix required for release."""
import json,pathlib,shutil,subprocess,time
root=pathlib.Path(__file__).resolve().parents[1];rows=[]
for version in ['24.04','26.04']:
    image='cryptowallet-package-test:'+version;start=time.monotonic()
    if not shutil.which('docker'):
        rows.append({'os':version,'status':'BLOCKED','reason':'Docker executable unavailable','seconds':0});continue
    try:
        build=subprocess.run(['docker','build','--build-arg','BASE=ubuntu:'+version,'-t',image,'-f','tests/e2e/Dockerfile','.'],cwd=root,timeout=600)
        if build.returncode:rows.append({'os':version,'status':'BLOCKED','reason':'disposable runtime image failed','seconds':time.monotonic()-start});continue
        p=subprocess.run(['docker','run','--rm',image],cwd=root,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=90)
        print(p.stdout,flush=True)
        rows.append({'os':version,'status':'PASS' if p.returncode==0 else 'BLOCKED','scope':'container Xvfb portable runtime startup only','reason':'' if p.returncode==0 else 'Inspect sandbox/native startup log; no weakening applied','seconds':round(time.monotonic()-start,3),'exit':p.returncode})
    except (OSError,subprocess.TimeoutExpired) as error:
        rows.append({'os':version,'status':'BLOCKED','reason':str(error),'seconds':round(time.monotonic()-start,3)})
report={'status':'NOT_RUN','reason':'Full real desktop flows, stock OS session lock, Wayland and upgrade/recovery matrix are not established by container smoke tests','containers':rows}
(root/'reports').mkdir(exist_ok=True);(root/'reports/desktop-e2e.json').write_text(json.dumps(report,indent=2)+'\n')
raise SystemExit(any(x['status']!='PASS' for x in rows) or report['status']!='PASS')
