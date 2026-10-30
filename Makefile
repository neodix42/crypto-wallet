.DEFAULT_GOAL := build
export GOTOOLCHAIN := go1.27.3
TOOLS := ./scripts/with-toolchain.sh
GO_TOOLS := ./scripts/with-go.sh
FUZZ_SECONDS ?= 60
SOAK_SECONDS ?= 86400
.PHONY: test-recovery-portable test-send-portable test-native-wallet test-wails-wallet test-native-wallet-legacy
.PHONY: frontend
.PHONY: test-wails-security
.PHONY: test-install-portable
.PHONY: test-near-staking test-near-staking-local test-near-staking-gui test-near-staking-testnet
.PHONY: test-erc20
.PHONY: test-near-tokens test-near-tokens-local test-near-tokens-gui
.PHONY: test-wallet-history test-wallet-history-gui
.PHONY: test-ci test-security-ci test-race-ci check-deps-ci
.PHONY: test-packaged-desktop test-packaged-send-desktop build-components install-desktop package-portable package-components test-portable build build-go build-desktop install dev test test-go test-security test-race test-interop test-integration test-native test-dialogs test-fuzz test-fuzz-long test-crash test-release-auth test-release-evidence test-license-notices test-packaged test-packaged-send test-recovery test-e2e test-testnet test-stress test-soak test-soak-non-decred test-exchange coverage package-desktop package-go verify-release check-deps
build:
	./scripts/build.sh
build-go: build
frontend:
	$(TOOLS) ./scripts/build-frontend.sh
# Fresh checkouts contain frontend sources, not an outdated embedded bundle.
# Build once per make invocation before Go tools load the webui go:embed files.
test test-go test-security test-race test-interop test-integration test-fuzz test-fuzz-long test-exchange check-deps coverage test-ci test-security-ci test-race-ci check-deps-ci: frontend
build-components:
	./scripts/build.sh --components
# Retained Electron regression runtime. New releases use package-portable.
build-desktop: build-components
	$(TOOLS) npm --prefix desktop ci --ignore-scripts
	$(TOOLS) npm --prefix desktop run build
	$(TOOLS) node --check desktop/ui/renderer.js
install: build
	python3 scripts/install.py
install-desktop: package-desktop
	python3 scripts/install.py --legacy-desktop
dev: install
	./cryptowallet
test:
	$(TOOLS) npm --prefix internal/webui/frontend test
	python3 scripts/build-tools-test.py
	python3 scripts/ci-go-test.py
	python3 scripts/crash-security-test.py
	python3 scripts/surface-check-test.py
	python3 scripts/mutation-test.py
	python3 scripts/near-token-mutation-test.py
	python3 internal/exchange/test-native-mutations-test.py
	python3 scripts/ci-desktop-test.py
	python3 scripts/desktop-startup-test.py
	python3 scripts/desktop-ocr-test.py
	python3 scripts/coverage-test.py
	python3 scripts/campaign-test.py
	python3 scripts/release-auth-test.py
	$(TOOLS) npm --prefix .github/provenance test
	python3 scripts/release-evidence-test.py
	python3 scripts/license-notices-test.py
	$(GO_TOOLS) go test ./...
	$(TOOLS) npm --prefix desktop run build
	$(TOOLS) npm --prefix desktop test
# Hosted checks exercise the shipped Wails UI and shared Go protocols. Full
# historical Electron/GTK suites and coverage remain available through the
# unchanged local targets above/below; these targets never depend on them.
test-ci:
	$(TOOLS) npm --prefix internal/webui/frontend test
	python3 scripts/build-tools-test.py --current-ui
	python3 scripts/ci-go-test.py
	python3 scripts/crash-security-test.py
	python3 scripts/surface-check-test.py
	python3 scripts/mutation-test.py
	python3 scripts/near-token-mutation-test.py
	python3 internal/exchange/test-native-mutations-test.py
	python3 scripts/ci-desktop-test.py
	python3 scripts/desktop-startup-test.py --current-ui
	python3 scripts/campaign-test.py
	python3 scripts/release-auth-test.py
	$(TOOLS) npm --prefix .github/provenance test
	python3 scripts/license-notices-test.py --current-ui
	$(GO_TOOLS) python3 scripts/ci-go.py test
test-go:
	$(GO_TOOLS) go test ./...
test-near-staking: frontend
	$(GO_TOOLS) go test -race ./internal/chains/near ./internal/signer ./internal/walletcore ./internal/nativewallet ./internal/webui -run Staking -count=1
	$(TOOLS) npm --prefix internal/webui/frontend test
	$(GO_TOOLS) python3 scripts/near-staking-mutation.py
test-near-staking-local:
	$(TOOLS) node tests/oracles/near/run-staking-local.mjs
test-near-staking-gui: build
	$(TOOLS) python3 scripts/wails-wallet-test.py --staking-only --report-name wails-near-staking
# Explicit opt-in; this never runs as part of the ordinary or CI test targets.
test-near-staking-testnet:
	WALLET_NEAR_STAKING_TESTNET=1 $(GO_TOOLS) go test ./tests/staking -run TestPublicTestnetStaking -v -count=1
	@python3 -c 'import json,pathlib,sys; r=json.loads(pathlib.Path("reports/near-staking-testnet.json").read_text()); print("Testnet cycle: " + r["status"] + ". " + r.get("reason", "")); sys.exit(0 if r["status"] == "PASS" else 1)'
test-erc20: frontend
	$(GO_TOOLS) go test -race ./internal/chains/evm ./internal/signer ./internal/walletcore ./internal/walletsigner ./internal/nativewallet ./internal/vault -run 'ERC20|Token' -count=1
	WALLET_ERC20_EVM=1 $(TOOLS) go test ./internal/chains/evm -run '^TestTP050ImportedERC20GoSignedTwoWayExecutionOnIndependentGanache$$' -v -count=1
test-near-tokens: frontend
	$(GO_TOOLS) go test -race ./internal/model ./internal/chains/near ./internal/vault ./internal/signer ./internal/walletsigner ./internal/walletcore ./internal/nativewallet -run 'NEP141|NearToken|Token' -count=1
	$(GO_TOOLS) python3 scripts/near-token-mutation.py
test-near-tokens-local:
	$(TOOLS) node tests/oracles/near/run-tokens-local.mjs
test-near-tokens-gui: build
	$(GO_TOOLS) python3 scripts/wails-wallet-test.py --near-tokens-only --report-name wails-near-tokens
test-wallet-history: frontend
	$(GO_TOOLS) go test -race ./internal/chains/... ./internal/model ./internal/journal ./internal/signer ./internal/walletsigner ./internal/walletcore ./internal/nativewallet ./internal/webui -run 'History|Journal|Explorer|TokenVerification|ExchangeSelection' -count=1
test-wallet-history-gui: build
	$(GO_TOOLS) python3 scripts/wails-wallet-test.py --history-layout-only --report-name wails-history-layout
test-exchange:
	$(GO_TOOLS) go test ./internal/exchange/...
	$(GO_TOOLS) go test ./internal/signer ./internal/walletsigner ./internal/walletcore ./internal/chains/evm -run 'Exchange|Bybit|USDT|USDC'
	$(GO_TOOLS) python3 internal/exchange/bybit/test-mutations.py
	$(GO_TOOLS) python3 scripts/exchange-mutation.py
	$(GO_TOOLS) python3 internal/exchange/test-native-mutations.py
	WALLET_USDT_EVM=1 $(TOOLS) go test ./internal/chains/evm -run USDT -count=1
	$(TOOLS) npm --prefix desktop run build
	$(TOOLS) npm --prefix desktop test
test-security:
	$(GO_TOOLS) go test ./internal/vault ./internal/signer ./internal/ipc ./internal/isolation ./internal/strictjson ./internal/amount ./internal/qr ./internal/uiprompt ./internal/journal ./internal/supervisor ./internal/walletcore ./internal/walletsigner ./cmd/wallet-launcher
	$(TOOLS) python3 scripts/native-input.py
	$(TOOLS) python3 scripts/mutation.py
	$(GO_TOOLS) python3 internal/exchange/bybit/test-mutations.py
	$(GO_TOOLS) python3 scripts/exchange-mutation.py
	$(GO_TOOLS) python3 internal/exchange/test-native-mutations.py
	$(GO_TOOLS) python3 scripts/near-token-mutation.py
	$(TOOLS) python3 scripts/surface-check.py
test-security-ci:
	$(GO_TOOLS) go test ./internal/vault ./internal/signer ./internal/ipc ./internal/isolation ./internal/strictjson ./internal/amount ./internal/qr ./internal/uiprompt ./internal/journal ./internal/supervisor ./internal/walletcore ./internal/walletsigner ./internal/webui
	$(TOOLS) python3 scripts/mutation.py
	$(GO_TOOLS) python3 internal/exchange/bybit/test-mutations.py
	$(GO_TOOLS) python3 scripts/exchange-mutation.py
	$(GO_TOOLS) python3 internal/exchange/test-native-mutations.py
	$(GO_TOOLS) python3 scripts/near-token-mutation.py
	$(TOOLS) python3 scripts/surface-check.py --current-ui
test-race:
	$(GO_TOOLS) go test -race -p 2 ./...
test-race-ci:
	$(GO_TOOLS) python3 scripts/ci-go.py race
test-interop:
	$(GO_TOOLS) go test ./internal/derivation ./internal/chains/... -run 'TP020|TP021|TP022|TP050|Independent|Oracle|Fixture|Vector|Interop'
test-integration:
	$(TOOLS) python3 scripts/integration.py
test-fuzz:
	$(TOOLS) python3 scripts/fuzz.py
test-fuzz-long:
	$(GO_TOOLS) python3 scripts/fuzz.py --seconds $(FUZZ_SECONDS) --exclude-package chains/decred
test-crash: build-components
	$(GO_TOOLS) python3 scripts/crash-security.py
test-release-auth:
	python3 scripts/release-auth-test.py
	$(TOOLS) npm --prefix .github/provenance test
test-release-evidence:
	python3 scripts/release-evidence-test.py
test-license-notices:
	python3 scripts/license-notices-test.py
test-dialogs:
	$(TOOLS) npm --prefix desktop run build
	$(TOOLS) npm --prefix desktop test
test-native:
	$(TOOLS) python3 scripts/native-input.py
test-wails-wallet: build
	$(GO_TOOLS) python3 scripts/wails-wallet-test.py
test-native-wallet: test-wails-wallet
test-wails-security: build
	$(GO_TOOLS) python3 scripts/verify-wails-fork.py
	$(GO_TOOLS) go -C third_party/wails test -race ./internal/frontend/walletclipboard ./internal/logger ./pkg/assetserver ./internal/frontend/dispatcher
	$(GO_TOOLS) python3 scripts/test-wails-security.py
	$(GO_TOOLS) python3 scripts/wails-inspector-test.py
test-install-portable: package-portable
	$(GO_TOOLS) python3 scripts/portable-install-test.py
test-native-wallet-legacy:
	$(GO_TOOLS) python3 scripts/native-wallet-test.py
test-packaged: package-portable
	$(GO_TOOLS) python3 scripts/wails-wallet-test.py
test-packaged-send: test-send-portable
test-packaged-desktop: install-desktop
	$(TOOLS) python3 scripts/packaged-recovery.py
	$(TOOLS) python3 scripts/phrase-backup-packaged.py
	$(TOOLS) python3 scripts/unlock-layout.py
test-packaged-send-desktop: install-desktop
	$(TOOLS) python3 scripts/packaged-send.py
test-recovery: build-components
	$(GO_TOOLS) python3 scripts/phrase-recovery.py
test-recovery-portable: build
	$(GO_TOOLS) python3 scripts/phrase-recovery.py --portable
test-send-portable: build
	$(GO_TOOLS) python3 scripts/portable-send.py
test-e2e: test-packaged-desktop
	$(TOOLS) python3 scripts/e2e.py
test-testnet:
	python3 scripts/testnet.py
test-stress:
	$(TOOLS) go test -timeout 10m ./tests/stress -run TestTP071 -count=1 -v
test-soak:
	$(TOOLS) go test -timeout 25h ./tests/stress -run TestTP072 -count=1 -v -args -soak=24h
test-soak-non-decred:
	$(GO_TOOLS) python3 scripts/run-soak.py --seconds $(SOAK_SECONDS)
coverage:
	$(TOOLS) python3 scripts/go-coverage.py
	$(TOOLS) python3 scripts/frontend-coverage.py
check-deps:
	$(TOOLS) go mod verify
	$(TOOLS) go vet ./...
	$(TOOLS) go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/ci.yml .github/workflows/release.yml
	$(TOOLS) go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/fuzz.yml
	$(TOOLS) go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	$(TOOLS) npm --prefix desktop audit --audit-level=high
	$(TOOLS) npm --prefix internal/webui/frontend audit --audit-level=high
	$(TOOLS) npm --prefix .github/provenance audit --audit-level=high
check-deps-ci:
	$(GO_TOOLS) go mod verify
	$(GO_TOOLS) python3 scripts/ci-go.py vet
	$(GO_TOOLS) go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/ci.yml .github/workflows/release.yml
	$(GO_TOOLS) go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/fuzz.yml
	$(GO_TOOLS) go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	$(TOOLS) npm --prefix internal/webui/frontend audit --audit-level=high
	$(TOOLS) npm --prefix .github/provenance audit --audit-level=high
package-desktop: build-desktop
	$(TOOLS) node scripts/package.cjs
	$(TOOLS) python3 scripts/sbom.py
	$(TOOLS) node scripts/verify-artifact.cjs
package-portable: build
	$(GO_TOOLS) python3 scripts/package-portable.py
package-go: package-portable
test-portable: package-portable
	$(GO_TOOLS) python3 scripts/package-portable.py --verify-only
# Backend-only legacy archive, retained for explicit development use.
package-components: build-components
	$(GO_TOOLS) python3 scripts/sbom.py --go-only
	$(GO_TOOLS) python3 scripts/package-go.py
	$(GO_TOOLS) python3 scripts/verify-version.py --go-only
verify-release:
	python3 scripts/verify-release.py
