#!/usr/bin/env python3
"""Explicit networked setup; subsequent integration workloads use local nodes."""
import os,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parents[1]
def run(args,env=None):subprocess.run(args,cwd=root,env=env,check=True)
for name in ['evm','near','aptos','ton']:run(['npm','ci','--ignore-scripts','--prefix','tests/oracles/'+name])
run(['sh','tests/oracles/near/install-sandbox.sh'])
run(['sh','tests/oracles/aptos/install-cli.sh'])
where=root/'.tools/dcrd';where.mkdir(parents=True,exist_ok=True)
run(['go','install','github.com/decred/dcrd@v1.10.9'],{**os.environ,'GOBIN':str(where)})
run(['docker','pull','ethereum/client-go:v1.17.9@sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109'])
