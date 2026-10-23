.PHONY: build dev test test-security test-race test-interop test-integration test-e2e test-testnet test-stress test-soak coverage package-deb verify-release check-deps
build:
	./scripts/build.sh
dev: package-deb
	./dist/LocalWallet-linux-x64/wallet-launcher
test:
	go test ./...
	npm --prefix desktop run build
	npm --prefix desktop test
test-security:
	go test ./internal/vault ./internal/signer ./internal/ipc ./internal/isolation ./internal/strictjson ./internal/amount ./internal/qr ./internal/journal
	python3 scripts/mutation.py
	python3 scripts/surface-check.py
test-race:
	go test -race ./...
test-interop:
	go test ./internal/derivation ./internal/chains/... -run 'TP020|TP021|TP022|TP050|Independent|Oracle|Fixture|Vector|Interop'
test-integration:
	python3 scripts/integration.py
test-e2e: build
	python3 scripts/e2e.py
test-testnet:
	python3 scripts/testnet.py
test-stress:
	go test -timeout 10m ./tests/stress -run TestTP071 -count=1 -v
test-soak:
	go test -timeout 25h ./tests/stress -run TestTP072 -count=1 -v -args -soak=24h
coverage:
	python3 scripts/frontend-coverage.py
	mkdir -p reports
	go test -covermode=atomic -coverprofile=reports/go.cover ./...
	go tool cover -html=reports/go.cover -o reports/go.html
	python3 scripts/coverage.py reports/go.cover
check-deps:
	go mod verify
	go vet ./...
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	npm --prefix desktop audit --audit-level=high
package-deb: build
	node scripts/package.cjs
verify-release:
	python3 scripts/verify-release.py
