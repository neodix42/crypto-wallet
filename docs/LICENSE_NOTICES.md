# Third-party redistribution texts

The build now produces actual upstream license, notice, copying, copyright and patent texts alongside the dependency inventory. This is a technical deliverable. Independent license classification, compatibility, obligation and redistribution review remains **NOT_RUN**. No automated allowlist grants legal approval.

```sh
./scripts/with-go.sh python3 scripts/license-notices.py --go-only
./scripts/with-toolchain.sh python3 scripts/license-notices.py \
  --desktop-runtime dist/CryptoWallet-linux-x64
python3 scripts/license-notices-test.py
```

Go-only collection never reads npm files or invokes Node/Electron. It emits `dist/go-THIRD-PARTY-NOTICES.txt` and `.json`. The complete desktop emits `dist/THIRD-PARTY-NOTICES.txt` and `.json`, and copies that same pair into the actual portable runtime before archiving. The text file contains verbatim upstream UTF-8 bytes separated by identified source headers; the JSON inventory records component/version, Go module directory checksum or pinned npm integrity, relative source filenames, byte lengths, SHA256 hashes and the complete text file's hash. It contains no machine-specific cache paths or timestamps.

Go selection uses `go list -deps` for the three shipped commands and verifies downloaded module contents with `go mod verify`. The current closure is 38 pinned modules, plus the selected Go toolchain's standard-library license/patent texts. Graph-only modules, test tools and retained unshipped GTK dependencies are not represented as redistributed runtime dependencies. License-family files are collected recursively within each selected module so bundled sublibrary attributions are retained.

Desktop selection adds the actual packaged Electron `LICENSE` and complete `LICENSES.chromium.html`. The current application has zero production npm packages. The collector checks the real ASAR for unlisted `node_modules` entries; future production npm dependencies must match the lockfile and supply license/notice texts whose bytes match the package actually in that ASAR. Development npm tools are not silently described as shipped application code.

Collection fails on missing primary license/copying texts, symlinked source paths, hard-linked text files, outside paths, nonregular/empty/binary/invalid-UTF-8 files, size/resource bounds, local Go module replacements, missing module checksums, changed npm versions, mismatched packaged texts or unlisted npm packages. Outputs replace regular owned artifact files atomically; existing symlink/hard-link targets are refused. Repeat collection of unchanged inputs produces identical text and JSON bytes.

The initial local run passed 13 regressions covering provenance, deterministic rendering, exact byte preservation, source/output path attacks, missing texts, Go-only separation, packaged npm identity and nested unlisted dependencies. Actual collection passed with 39 Go components/56 texts and 40 desktop components/58 texts. These results are tracked in `reports/license-notices.json`; packaging/release inventories bind the generated files separately.

This collection does not determine whether source-offer, relinking, attribution, patent, modification-disclosure or other obligations apply or have been satisfied. In particular, copying LGPL text does not by itself satisfy all applicable LGPL obligations. System libc/libseccomp2 and graphical shared libraries are runtime prerequisites and are not copied from the host into these inventories; their use and any redistribution need separate review. This artifact also assigns no license to CryptoWallet's own source. Publisher signatures authenticate an artifact publisher, not the legal conclusions that remain outstanding.
