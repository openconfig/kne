DOCKER_IMAGE := networkop/meshnet
GOPATH ?= ${HOME}/go
ARCHS := "linux/amd64,linux/arm64"
#ARCHS := "linux/amd64"

COMMIT := $(shell git describe --dirty --always)
TAG := $(shell git describe --tags --abbrev=0 || echo latest)

include .mk/kind.mk
include .mk/ci.mk
include .mk/kustomize.mk
include .mk/buf.mk

.PHONY: all
all: docker

## Run unit tests
test:
	go test ./...

## Run unit tests for Reconciliation
recon-test:
	sudo go test -count=1 -v -run '' github.com/networkop/meshnet-cni/daemon/grpcwire
	#sudo go test -count=1 -run '' github.com/networkop/meshnet-cni/daemon/grpcwire

# Build local binaries
local-build:
	CGO_ENABLED=0 GOOS=linux go build -o meshnet github.com/networkop/meshnet-cni/plugin 
	CGO_ENABLED=1 GOOS=linux go build -o meshnetd github.com/networkop/meshnet-cni/daemon

# Remove local binaries
local-clean:
	@[ -f ./meshnet ] && rm meshnet || true
	@[ -f ./meshnetd ] && rm meshnetd || true


.PHONY: docker
## Build the docker image
docker:
	@echo 'Creating docker image ${DOCKER_IMAGE}:${COMMIT}'
	docker buildx create --use --name=multiarch --driver-opt network=host --buildkitd-flags '--allow-insecure-entitlement network.host' --node multiarch && \
	docker buildx build --load \
	--build-arg LDFLAGS=${LDFLAGS} \
	--platform "linux/amd64" \
	--tag ${DOCKER_IMAGE}:${COMMIT} \
	-f docker/Dockerfile \
	.


.PHONY: release
## Release the current code with git tag and `latest`
release:
	docker run --rm --privileged tonistiigi/binfmt --install all
	docker buildx build --push \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform ${ARCHS} \
		-t ${DOCKER_IMAGE}:${TAG} \
		-t ${DOCKER_IMAGE}:latest \
		-f docker/Dockerfile \
		.

## Generate GRPC code
proto: buf-generate

## Targets below are for integration testing only

.PHONY: up
## Build test environment
up: kind-start

.PHONY: down
## Desroy test environment
down: kind-stop

.PHONY: e2e
## Run the end-to-end test
e2e: wait-for-meshnet
	kubectl apply -f tests/3node.yml
	kubectl wait --timeout=120s --for condition=Ready pod -l test=3node 
	kubectl exec r1 -- ping -c 1 12.12.12.2
	kubectl exec r1 -- ping -c 1 13.13.13.3
	kubectl exec r2 -- ping -c 1 23.23.23.3

.PHONY: e2e-mlink
## Run the end-to-end test with multi-links betwwen a pair of nodes. 
e2e-mlink: wait-for-meshnet
	kubectl apply -f tests/3node-mlink.yml
	kubectl wait --timeout=120s --for condition=Ready pod -l test=3node-mlink --namespace=mlink
	kubectl exec r1 -n mlink -- ping -c 1 10.10.10.2
	kubectl exec r1 -n mlink -- ping -c 1 11.11.11.2
	kubectl exec r1 -n mlink -- ping -c 1 12.12.12.2
	kubectl exec r1 -n mlink -- ping -c 1 13.13.13.2
	kubectl exec r1 -n mlink -- ping -c 1 20.20.20.2
	kubectl exec r1 -n mlink -- ping -c 1 21.21.21.2
	kubectl exec r1 -n mlink -- ping -c 1 22.22.22.2
	kubectl exec r1 -n mlink -- ping -c 1 23.23.23.2
	kubectl exec r2 -n mlink -- ping -c 1 30.30.30.2
	kubectl exec r2 -n mlink -- ping -c 1 31.31.31.2
	kubectl exec r2 -n mlink -- ping -c 1 32.32.32.2
	kubectl exec r2 -n mlink -- ping -c 1 33.33.33.2

wait-for-meshnet:
	kubectl wait --for condition=Ready pod -l name=meshnet -n meshnet   
	sleep 5

.PHONY: install
## Install meshnet into a test cluster
install: kind-load kind-wait-for-cni kustomize kind-connect
ifdef grpc
	kustomize build manifests/overlays/grpc-link-e2e  | kubectl apply -f -
else
	kustomize build manifests/overlays/e2e | kubectl apply -f -
endif

.PHONY: uninstall
## Uninstall meshnet from a test cluster
uninstall: kind-connect
ifdef grpc
	-kustomize build manifests/overlays/grpc-link-e2e  | kubectl delete -f -
else
	-kustomize build manifests/overlays/e2e | kubectl delete -f -
endif

github-ci: kust-ensure build clean local upload install e2e

# From: https://gist.github.com/klmr/575726c7e05d8780505a
help:
	@echo "$$(tput sgr0)";sed -ne"/^## /{h;s/.*//;:d" -e"H;n;s/^## //;td" -e"s/:.*//;G;s/\\n## /---/;s/\\n/ /g;p;}" ${MAKEFILE_LIST}|awk -F --- -v n=$$(tput cols) -v i=15 -v a="$$(tput setaf 6)" -v z="$$(tput sgr0)" '{printf"%s%*s%s ",a,-i,$$1,z;m=split($$2,w," ");l=n-i;for(j=1;j<=m;j++){l-=length(w[j])+1;if(l<= 0){l=n-i-length(w[j])-1;printf"\n%*s ",-i," ";}printf"%s ",w[j];}printf"\n";}'
