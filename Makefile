MESHNET_DOCKER_IMAGE := hfam/meshnet
GOPATH ?= ${HOME}/go
KNE_CLI_BIN := kne
INSTALL_DIR := /usr/local/bin

COMMIT := $(shell git describe --dirty --always)
TAG := $(shell git describe --tags --abbrev=0 || echo latest)


include .mk/kind.mk
include .mk/lint.mk
include .mk/ocipush.mk

.PHONY: all
all: docker

## Run unit tests
test:
	go test ./...

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
	mv $(KNE_CLI_BIN) $(INSTALL_DIR)
