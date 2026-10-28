`english.txt` is the standard public 2048-word English BIP39 dictionary from
[BIP-0039](https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt),
identical to the dictionary in the pinned `go-bip39` v1.1.0 dependency.
Its SHA-256 is
`2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda`.

The Wails GUI acceptance driver uses this public data and an independent Python
checksum calculation to reject incomplete OCR before entering a disposable
generated phrase. Private pixels and words remain in RAM. The full campaign
also checks exact phrase equality through an explicitly authorized backup copy,
then compares every network address after restoring a separate installation.

`fixtures.json` remains the independent ethers 6.17.0 derivation/NFKD oracle
produced by `generate.mjs`, using public synthetic entropy and passphrases only.
