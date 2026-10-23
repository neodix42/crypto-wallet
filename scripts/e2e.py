#!/usr/bin/env python3
"""Real .deb installation/startup in disposable containers. This is explicitly
not the clean desktop X11+Wayland acceptance matrix required for release."""
import json,pathlib,subprocess,time
root=pathlib.Path(__file__).resolve().parents[1];rows=[]
for version in ['24.04','26.04']:
    image='localwallet-package-test:'+version;start=time.monotonic()
    build=subprocess.run(['docker','build','--build-arg','BASE=ubuntu:'+version,'-t',image,'-f','tests/e2e/Dockerfile','.'],cwd=root)
    if build.returncode:rows.append({'os':version,'status':'BLOCKED','reason':'disposable install image failed','seconds':time.monotonic()-start});continue
    p=subprocess.run(['docker','run','--rm',image],cwd=root,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=90)
    print(p.stdout,flush=True)
    rows.append({'os':version,'status':'PASS' if p.returncode==0 else 'BLOCKED','scope':'container Xvfb install/startup only','reason':'' if p.returncode==0 else 'Inspect sandbox/native startup log; no weakening applied','seconds':round(time.monotonic()-start,3),'exit':p.returncode})
report={'status':'NOT_RUN','reason':'Full real desktop flows, stock OS session lock, Wayland and upgrade/recovery matrix are not established by container smoke tests','containers':rows}
(root/'reports').mkdir(exist_ok=True);(root/'reports/desktop-e2e.json').write_text(json.dumps(report,indent=2)+'\n')
raise SystemExit(any(x['status']!='PASS' for x in rows) or report['status']!='PASS')
