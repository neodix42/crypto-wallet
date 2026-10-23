"""TEST-ONLY PUBLIC SYNTHETIC VECTORS, official hyperliquid-python-sdk 0.24.0.
SDK tag commit 2fdb18f9517675ea03695a0962bd19eece9c83f0.
Never pass user credentials or a funded key to this generator.
"""
import json
from pathlib import Path
from eth_account import Account
from eth_account.messages import encode_typed_data
from eth_utils import keccak
from hyperliquid.utils.signing import sign_spot_transfer_action, user_signed_payload, SPOT_TRANSFER_SIGN_TYPES

# SDK doesn't derive BIP32; consume independent ethers' public-test seed derivation.
# The fixture's test private key is derived independently with eth-account BIP32.
from eth_account.hdaccount import key_from_seed
seed=bytes(range(64))
fixtures=[]
for index in [0,1,127]:
    key=key_from_seed(seed, f"m/44'/60'/0'/0/{index}")
    wallet=Account.from_key(key)
    for mainnet in [True,False]:
        token="HYPE:0x0d01dc56dcaaca66ad901c959b4011ec" if mainnet else "HYPE:0x7317beb7cceed72ef0b346074cc8e7ab"
        action={"type":"spotSend","destination":"0x1111111111111111111111111111111111111111","token":token,"amount":"0.12345678","time":1792733000000}
        sig=sign_spot_transfer_action(wallet,action,mainnet)
        message=encode_typed_data(full_message=user_signed_payload("HyperliquidTransaction:SpotSend",SPOT_TRANSFER_SIGN_TYPES,action))
        digest=keccak(b"\x19"+message.version+message.header+message.body)
        fixtures.append({"index":index,"mainnet":mainnet,"address":wallet.address,"action":action,"hash":"0x"+digest.hex(),"signature":sig})
Path(__file__).with_name('fixtures.json').write_text(json.dumps({"oracle":"hyperliquid-python-sdk 0.24.0","commit":"2fdb18f9517675ea03695a0962bd19eece9c83f0","seed":seed.hex(),"fixtures":fixtures},indent=2)+'\n')
