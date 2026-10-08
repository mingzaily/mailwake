.PHONY: web build test check check-webdist check-build
VERSION ?= devel
LDFLAGS := -s -w -X github.com/mingzaily/mailwake/internal/buildinfo.Version=$(VERSION)

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -tags nomsgpack -trimpath -ldflags="$(LDFLAGS)" -o bin/mailwake ./cmd/mailwake

check-build: build
	$(MAKE) check-webdist

check-webdist:
	node web/scripts/check-webdist.mjs

test:
	cd web && npm test
	go test -tags nomsgpack,mailwake_test -race ./...

check:
	cd web && npm run check
	go vet -tags nomsgpack ./...
	go vet -tags nomsgpack,mailwake_test ./...
