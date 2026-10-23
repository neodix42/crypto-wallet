.DEFAULT_GOAL := build
export GOTOOLCHAIN := go1.27.3
TOOLS := ./scripts/with-toolchain.sh
.PHONY: build install dev test test-security test-race test-interop test-integration test-native test-fuzz test-e2e test-testnet test-stress test-soak coverage package-deb verify-release check-deps
build:
	./scripts/build.sh
install: package-deb
	python3 scripts/install.py
dev: install
	./wallet
test:
	python3 scripts/build-tools-test.py
	python3 scripts/coverage-test.py
	$(TOOLS) go test ./...
	$(TOOLS) npm --prefix desktop run build
	$(TOOLS) npm --prefix desktop test
test-security:
	$(TOOLS) go test ./internal/vault ./internal/signer ./internal/ipc ./internal/isolation ./internal/strictjson ./internal/amount ./internal/qr ./internal/journal ./internal/supervisor ./cmd/wallet-core ./cmd/wallet-signer ./cmd/wallet-launcher
	$(TOOLS) python3 scripts/native-input.py
	$(TOOLS) python3 scripts/mutation.py
	$(TOOLS) python3 scripts/surface-check.py
test-race:
	$(TOOLS) go test -race -p 2 ./...
test-interop:
	$(TOOLS) go test ./internal/derivation ./internal/chains/... -run 'TP020|TP021|TP022|TP050|Independent|Oracle|Fixture|Vector|Interop'
test-integration:
	$(TOOLS) python3 scripts/integration.py
test-fuzz:
	$(TOOLS) python3 scripts/fuzz.py
test-native:
	$(TOOLS) python3 scripts/native-input.py
test-e2e: package-deb
	$(TOOLS) python3 scripts/packaged-recovery.py
	$(TOOLS) python3 scripts/e2e.py
test-testnet:
	python3 scripts/testnet.py
test-stress:
	$(TOOLS) go test -timeout 10m ./tests/stress -run TestTP071 -count=1 -v
test-soak:
	$(TOOLS) go test -timeout 25h ./tests/stress -run TestTP072 -count=1 -v -args -soak=24h
coverage:
	$(TOOLS) python3 scripts/go-coverage.py
	$(TOOLS) python3 scripts/frontend-coverage.py
check-deps:
	$(TOOLS) go mod verify
	$(TOOLS) go vet ./...
	$(TOOLS) go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/ci.yml .github/workflows/release.yml .github/workflows/soak.yml
	$(TOOLS) go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	$(TOOLS) npm --prefix desktop audit --audit-level=high
package-deb: build
	$(TOOLS) node scripts/package.cjs
	$(TOOLS) python3 scripts/sbom.py
	$(TOOLS) node scripts/verify-artifact.cjs
verify-release:
	python3 scripts/verify-release.py
