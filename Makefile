MESHNET_DOCKER_IMAGE := hfam/meshnet
GOPATH ?= ${HOME}/go
KNE_CLI_BIN := kne
INSTALL_DIR := /usr/local/bin

COMMIT := $(shell git describe --dirty --always)
# Allow overriding TAG via env (e.g. `TAG=v0.4.0-dn make dist`) so release
# builds aren't pinned to whatever `git describe` happens to return.
TAG ?= $(shell git describe --tags --abbrev=0 || echo latest)


include .mk/kind.mk
include .mk/lint.mk
include .mk/ocipush.mk

.PHONY: all
all: docker

## Run unit tests
## Ignore all tests under the cloudbuild/ tree as these targets are end-to-end
test:
	go test `go list ./... | grep -v /cloudbuild`

## Targets below are for integration testing only

.PHONY: up
## Build test environment
up: kind-start

.PHONY: down
## Destroy test environment
down: kind-stop

.PHONY: build
## Build kne
build:
	CGO_ENABLED=0 go build -o $(KNE_CLI_BIN) -ldflags="-s -w" kne_cli/main.go

.PHONY: install
## Install kne cli binary to user's local bin dir
install: build
	sudo mv $(KNE_CLI_BIN) $(INSTALL_DIR)

.PHONY: dist
## Cross-build kne_cli for linux+darwin (amd64+arm64) into ./dist
## Mirrors what .github/workflows/release.yml does so releases are reproducible locally.
dist:
	@rm -rf dist && mkdir -p dist
	@COMMIT=$$(git rev-parse --short HEAD); \
	 DATE=$$(git show -s --format=%cI HEAD); \
	 LDFLAGS="-s -w -X main.version=$(TAG) -X main.commit=$$COMMIT -X main.date=$$DATE"; \
	 for combo in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
	   os=$${combo%/*}; arch=$${combo#*/}; \
	   out=dist/kne_$(TAG)_$${os}_$${arch}; \
	   mkdir -p "$$out"; \
	   echo "==> $$out (GOOS=$$os GOARCH=$$arch)"; \
	   CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
	     go build -trimpath -ldflags="$$LDFLAGS" \
	              -o "$$out/$(KNE_CLI_BIN)" ./kne_cli; \
	   cp LICENSE "$$out/" 2>/dev/null || true; \
	   cp README.md "$$out/" 2>/dev/null || true; \
	   tar -C dist -czf "dist/kne_$(TAG)_$${os}_$${arch}.tar.gz" "$$(basename $$out)"; \
	   rm -rf "$$out"; \
	 done; \
	 ( cd dist && sha256sum -- *.tar.gz > SHA256SUMS )
	@ls -lh dist/
