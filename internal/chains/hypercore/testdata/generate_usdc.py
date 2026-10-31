"""TEST ONLY: public synthetic seed; never use a funded key or user secret.

Reproduce with the hash-locked tests/oracles/hypercore/requirements.lock.
Official hyperliquid-python-sdk 0.24.0, upstream tag commit
2fdb18f9517675ea03695a0962bd19eece9c83f0. No HTTP or submission occurs.
"""
import json
from pathlib import Path
from eth_account import Account
from eth_account.hdaccount import key_from_seed
from eth_account.messages import encode_typed_data
from eth_utils import keccak
from hyperliquid.utils.signing import (
    SPOT_TRANSFER_SIGN_TYPES, sign_spot_transfer_action, user_signed_payload,
)
seed = bytes(range(64))
fixtures = []
for index in [0, 1, 127]:
    key = key_from_seed(seed, f"m/44'/60'/0'/0/{index}")
    wallet = Account.from_key(key)
    action = {
        "type": "spotSend", "destination": "0x1111111111111111111111111111111111111111",
        "token": "USDC:0x6d1e7cde53ba9467b783cb7c530ce054",
        "amount": "0.12345678", "time": 1793404800000,
    }
    signature = sign_spot_transfer_action(wallet, action, True)
    message = encode_typed_data(full_message=user_signed_payload(
        "HyperliquidTransaction:SpotSend", SPOT_TRANSFER_SIGN_TYPES, action,
    ))
    digest = keccak(b"\x19" + message.version + message.header + message.body)
    fixtures.append({"index": index, "address": wallet.address, "action": action,
                     "hash": "0x" + digest.hex(), "signature": signature})
Path(__file__).with_name("usdc-sdk.json").write_text(json.dumps({
    "oracle": "hyperliquid-python-sdk 0.24.0",
    "commit": "2fdb18f9517675ea03695a0962bd19eece9c83f0",
    "seed": seed.hex(), "fixtures": fixtures,
}, indent=2) + "\n")
