MESHNET_DOCKER_IMAGE := hfam/meshnet
GOPATH ?= ${HOME}/go

COMMIT := $(shell git describe --dirty --always)
TAG := $(shell git describe --tags --abbrev=0 || echo latest)


include .mk/kind.mk

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
## Desroy test environment
down: kind-stop
