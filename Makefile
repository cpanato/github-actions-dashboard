.PHONY: test lint verify ko-local

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run
	golangci-lint fmt --diff

verify:
	go mod verify
	go mod tidy
	git diff --exit-code -- go.mod go.sum
	go vet ./...
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

ko-local:
	KO_DOCKER_REPO=ko.local LDFLAGS="$(LDFLAGS)" \
	KOCACHE=$(KOCACHE_PATH) ko build --base-import-paths \
		github.com/cpanato/github-actions-dashboard
