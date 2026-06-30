# KIND cluster name
KIND_CLUSTER_NAME := "meshnet"

.PHONY: kind-install
kind-install:
	go install sigs.k8s.io/kind@v0.17.0

.PHONY: kind-stop
kind-stop:
	@$(GOPATH)/bin/kind delete cluster --name $(KIND_CLUSTER_NAME) || \
		echo "kind cluster is not running"

.PHONY: kind-ensure
kind-ensure:
	@which $(GOPATH)/bin/kind >/dev/null 2>&1 || \
		make kind-install

.PHONY: kind-start
kind-start: kind-ensure
	@$(GOPATH)/bin/kind get clusters | grep $(KIND_CLUSTER_NAME)  >/dev/null 2>&1 || \
		$(GOPATH)/bin/kind create cluster --name $(KIND_CLUSTER_NAME) --config ./kind.yaml

.PHONY: kind-wait-for-cni
kind-wait-for-cni:
	kubectl wait --timeout=60s --for condition=Ready pod -l app=kindnet -n kube-system

.PHONY: kind-connect
kind-connect:
	kubectl cluster-info --context kind-meshnet >/dev/null

.PHONY: kind-load
kind-load:
	$(GOPATH)/bin/kind load docker-image --name $(KIND_CLUSTER_NAME) ${DOCKER_IMAGE}:${COMMIT}
