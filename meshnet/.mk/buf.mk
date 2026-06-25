# buf-specific targets. See https://github.com/bufbuild/buf
# NOTE: make sure you've got protoc-gen-go installed. See https://grpc.io/docs/languages/go/quickstart/

.PHONY: buf-ensure 
buf-ensure: 
	@which buf >/dev/null 2>&1 || \
		echo 'Install buf with "go get github.com/bufbuild/buf/cmd/buf@latest"'

.PHONY: lint
buf-lint: buf-ensure 
	buf lint

.PHONY: buf-generate
buf-generate: buf-lint
	buf generate -v