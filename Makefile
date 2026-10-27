.DEFAULT_GOAL := build
export GOTOOLCHAIN := go1.27.3
TOOLS := ./scripts/with-toolchain.sh
GO_TOOLS := ./scripts/with-go.sh
FUZZ_SECONDS ?= 60
SOAK_SECONDS ?= 86400
.PHONY: build build-go build-desktop install dev test test-go test-security test-race test-interop test-integration test-native test-dialogs test-fuzz test-fuzz-long test-crash test-release-auth test-release-evidence test-license-notices test-packaged test-packaged-send test-recovery test-e2e test-testnet test-stress test-soak test-soak-non-decred test-exchange coverage package-desktop package-go verify-release check-deps
build:
	./scripts/build.sh
build-go: build
build-desktop: build
	$(TOOLS) npm --prefix desktop ci --ignore-scripts
	$(TOOLS) npm --prefix desktop run build
	$(TOOLS) node --check desktop/ui/renderer.js
install: package-desktop
	python3 scripts/install.py
dev: install
	./wallet
test:
	python3 scripts/build-tools-test.py
	python3 scripts/mutation-test.py
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
test-go:
	$(GO_TOOLS) go test ./...
test-exchange:
	$(GO_TOOLS) go test ./internal/exchange/...
	$(GO_TOOLS) go test ./internal/signer ./cmd/wallet-signer ./cmd/wallet-core ./internal/chains/evm -run 'Exchange|Bybit|USDT'
	$(GO_TOOLS) python3 internal/exchange/bybit/test-mutations.py
	$(GO_TOOLS) python3 scripts/exchange-mutation.py
	WALLET_USDT_EVM=1 $(TOOLS) go test ./internal/chains/evm -run USDT -count=1
	$(TOOLS) npm --prefix desktop run build
	$(TOOLS) npm --prefix desktop test
test-security:
	$(GO_TOOLS) go test ./internal/vault ./internal/signer ./internal/ipc ./internal/isolation ./internal/strictjson ./internal/amount ./internal/qr ./internal/uiprompt ./internal/journal ./internal/supervisor ./cmd/wallet-core ./cmd/wallet-signer ./cmd/wallet-launcher
	$(TOOLS) python3 scripts/native-input.py
	$(TOOLS) python3 scripts/mutation.py
	$(GO_TOOLS) python3 internal/exchange/bybit/test-mutations.py
	$(GO_TOOLS) python3 scripts/exchange-mutation.py
	$(TOOLS) python3 scripts/surface-check.py
test-race:
	$(GO_TOOLS) go test -race -p 2 ./...
test-interop:
	$(GO_TOOLS) go test ./internal/derivation ./internal/chains/... -run 'TP020|TP021|TP022|TP050|Independent|Oracle|Fixture|Vector|Interop'
test-integration:
	$(TOOLS) python3 scripts/integration.py
test-fuzz:
	$(TOOLS) python3 scripts/fuzz.py
test-fuzz-long:
	$(GO_TOOLS) python3 scripts/fuzz.py --seconds $(FUZZ_SECONDS) --exclude-package chains/decred
test-crash: build
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
test-packaged: install
	$(TOOLS) python3 scripts/packaged-recovery.py
	$(TOOLS) python3 scripts/phrase-backup-packaged.py
	$(TOOLS) python3 scripts/unlock-layout.py
test-packaged-send: install
	$(TOOLS) python3 scripts/packaged-send.py
test-recovery: build
	$(GO_TOOLS) python3 scripts/phrase-recovery.py
test-e2e: test-packaged
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
	$(TOOLS) npm --prefix .github/provenance audit --audit-level=high
package-desktop: build-desktop
	$(TOOLS) node scripts/package.cjs
	$(TOOLS) python3 scripts/sbom.py
	$(TOOLS) node scripts/verify-artifact.cjs
package-go: build
	$(GO_TOOLS) python3 scripts/sbom.py --go-only
	$(GO_TOOLS) python3 scripts/package-go.py
	$(GO_TOOLS) python3 scripts/verify-version.py --go-only
verify-release:
	python3 scripts/verify-release.py
