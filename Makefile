.PHONY: test test-js test-integration test-e2e assets lint verify ko-local

test:
	go test -race -count=1 ./...

test-js:
	npm test

test-integration:
	@test -n "$(DASHBOARD_TEST_DATABASE_URL)" || (echo "Set DASHBOARD_TEST_DATABASE_URL to a loopback PostgreSQL database ending in _test"; exit 1)
	go test -race -count=1 ./...

test-e2e:
	npm run test:e2e

assets:
	npm ci
	npm run build:assets

lint:
	golangci-lint run

verify:
	go mod verify
	go mod tidy
	git diff --exit-code -- go.mod go.sum
	go vet ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ko-local:
	KO_DOCKER_REPO=ko.local LDFLAGS="$(LDFLAGS)" \
	KOCACHE=$(KOCACHE_PATH) ko build --base-import-paths \
		github.com/cpanato/github-actions-dashboard
