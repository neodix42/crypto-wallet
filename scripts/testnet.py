#!/usr/bin/env python3
"""Public testnet gates are intentionally separate from hermetic tests.
This runner never obtains credentials from an environment or an argument and
cannot spend funds. Live receipts must come from the native-approved wallet
and be independently verified against the exact candidate, not synthesized.
"""
import json,pathlib
root=pathlib.Path(__file__).resolve().parents[1]
rows=[{'network':n,'status':'NOT_RUN','reason':'No native-approved dedicated testnet roundtrip/independent receipt evidence supplied for this candidate'} for n in ['ethereum-sepolia','hyperevm-testnet','hypercore-testnet','base-sepolia-ZEN','horizen-testnet-ZEN','near-testnet','aptos-testnet','decred-testnet3','ton-testnet']]
report={'status':'NOT_RUN','networks':rows,'nextAction':'Use disposable capped testnet profiles and native approval; retain signed identity, successful execution/finality and independent recipient state for send and send-back. Never run this on mainnet.'}
(root/'reports').mkdir(exist_ok=True);(root/'reports/public-testnets.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2));raise SystemExit(1)
