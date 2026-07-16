.PHONY: kust-install
kust-install:
	go install sigs.k8s.io/kustomize/kustomize/v5@v5.0.0

.PHONY: kust-ensure
kust-ensure:
	@which $(GOPATH)/bin/kustomize >/dev/null 2>&1 || \
		make kust-install

.PHONY: yq-install
yq-install:
	go install github.com/mikefarah/yq/v4@v4.44.3

.PHONY: yq-ensure
yq-ensure:
	@which $(GOPATH)/bin/yq >/dev/null 2>&1 || \
		make yq-install

.PHONY: kustomize
kustomize: kust-ensure yq-ensure
	cd manifests/overlays/e2e && $(GOPATH)/bin/kustomize edit set image us-west1-docker.pkg.dev/kne-external/kne/meshnet=${DOCKER_IMAGE}:${COMMIT} && $(GOPATH)/bin/yq -i -I 2 '.' kustomization.yaml
	cd manifests/overlays/grpc-link-e2e && $(GOPATH)/bin/kustomize edit set image us-west1-docker.pkg.dev/kne-external/kne/meshnet=${DOCKER_IMAGE}:${COMMIT} && $(GOPATH)/bin/yq -i -I 2 '.' kustomization.yaml
	cd manifests/overlays/grpc-link && $(GOPATH)/bin/kustomize edit set image us-west1-docker.pkg.dev/kne-external/kne/meshnet=${DOCKER_IMAGE}:${COMMIT} && $(GOPATH)/bin/yq -i -I 2 '.' kustomization.yaml


.PHONY: kustomize-kops
kustomize-kops: kust-ensure
	kubectl apply -k manifests/overlays/kops/
