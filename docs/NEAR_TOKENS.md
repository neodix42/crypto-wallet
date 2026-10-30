# NEAR fungible tokens

NEAR wallets support explicitly tracked NEP-141 fungible tokens in addition to
native NEAR and staking. Base already supports additional ERC-20 tokens; see
[Base token behavior and evidence](ERC20.md). These are separate networks and
token standards, even if a token uses the same symbol.

## Track, receive and send

1. Open the NEAR wallet and enter the exact contract account under **Track a
   NEAR token**. Choose **Verify and add token**. Go reads its metadata and code
   hash on the checked network, then requires confirmation and the current app
   password before saving the policy in the encrypted vault.
2. Select its **Token / Balance / Decimals** row. Hover or focus the token button
   to see its exact network/contract. Balances include observed zeroes; failed
   observations stay unknown without replacing other verified asset balances.
3. **Receive** shows the owned account's address and QR. The contract account
   identifies the token, not your destination. **Check token registration**
   reads that token's storage state for this owned address.
4. If unregistered, **Register to receive token** prepares a self-only registration
   with the observed storage minimum. Review the exact contract, deposit and
   estimated native fee, then use **Review and register** and enter the app
   password. Registration spends native NEAR separately from any token payment.
   A sender may register the receiving address instead. Fund the implicit wallet
   with native NEAR before asking it to register or send.
5. **Send** uses whole-token decimal amounts: `1.25` means 1.25 tokens at their
   approved precision. Both sender and recipient must already be registered.
   Native NEAR pays gas and the transfer's one-yocto deposit. Every payment has
   its own exact approval/password and durable operation identity.

This follows [NEAR's fungible-token interface](https://docs.near.org/primitives/ft/standard),
[NEP-141](https://github.com/near/NEPs/blob/master/neps/nep-0141.md),
[NEP-145](https://github.com/near/NEPs/blob/master/neps/nep-0145.md) and
[NEP-148](https://github.com/near/NEPs/blob/master/neps/nep-0148.md).

## Exact supported behavior

The signer reconstructs one Borsh FunctionCall targeting the approved contract.
It permits only `ft_transfer` with canonical recipient/amount arguments, exactly
one yoctoNEAR and fixed 30 Tgas, or `storage_deposit` with the owned account,
`registration_only: true`, the observed exact storage minimum and fixed 30 Tgas.
Storage registration is capped at one NEAR and requires a positive standard
minimum. Unknown/malformed storage requirements cannot authorize spending.
No arbitrary methods, `ft_transfer_call`, callbacks, unregister, storage
withdrawal, wrapping/unwrapping, NFTs or new Bybit routes are enabled.

Amounts are canonical unsigned 128-bit integers, independently of native NEAR's
24 decimals. Import requires bounded standard metadata, a named contract account,
a supported 0–77 decimal precision and a 1–24 character ASCII symbol consisting
of letters, digits, dot, underscore or hyphen. At most 32 policies are retained
per profile. Metadata images/reference URLs are never fetched. There is no
address-wide discovery claim: add each desired exact contract yourself.

Final-block RPC observations recheck approved code and metadata before displaying
token balances, preparing or broadcasting. A code/precision change stays
unsupported/unknown; an existing watch policy is never silently updated. A
familiar symbol, matching code hash or contract success cannot establish issuer
legitimacy or economic value. RPC responses remain provider observations, not an
independent consensus proof. Standard logs and receipts can be fabricated by a
malicious contract; the tested transfer behavior applies to the pinned standard
fixture, not every contract returning similar metadata.

Full-access keys, nonce reservations, expiry, native account storage reserve and
current gas checks remain required. NEAR does not sign a maximum gas price; the
displayed fee reserve is an estimate. Signed bytes are saved before submission.
Success requires the original transaction and complete successful receipt graph,
plus the exact token-transfer event or standard self-registration result. An
ambiguous response retains UNKNOWN and its reservation. Reconcile the original
operation in **Settings → Journal**; do not create a replacement payment.
The local journal includes these token operations. The separate bounded NEAR
chain-history scan currently covers native transfers, not arbitrary token history.

## Recovery and tests

The same phrase/profile derives the same NEAR address and token ownership with
either the same or a different local app password. A fresh phrase restore does
not recreate local watch approvals or the payment journal. Add each token
contract again; its on-chain registration can be read again without another
registration deposit. Existing native-only vaults remain readable.

```sh
make test-near-tokens
make test-near-tokens-local
make test-near-tokens-gui
```

The local-node command uses the pinned sandbox and official FT contract fixture;
provision its dependencies as described in [NEAR adapter evidence](NEAR_APTOS.md).
The graphical command deliberately has no successful RPC service or fabricated
token balances: it checks the actual import controls, native table, validation,
honest offline failure, Send units and Staking navigation. It does not prove a
graphically approved funded token transfer.

Current PASS/FAIL/BLOCKED/NOT_RUN results, exact commands and candidate scope are
recorded in [STATUS.md](STATUS.md). Public funded token transfers, the clean OS
matrix and independent review are separate gates. Development uses disposable
profiles/public fixtures and never automatically spends mainnet funds.
