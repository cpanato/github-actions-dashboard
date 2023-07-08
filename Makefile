

.PHONY: ko-local
ko-local:
	KO_DOCKER_REPO=ko.local LDFLAGS="$(LDFLAGS)" \
	KOCACHE=$(KOCACHE_PATH) ko build --base-import-paths \
		github.com/cpanato/github-actions-dashboard
