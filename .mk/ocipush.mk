# build kne and push it as an OCI artifact to ttl.sh
# to obtain the pushed artifact use:
# docker run --rm -v $(pwd):/workspace ghcr.io/deislabs/oras:v0.11.1 pull ttl.sh/<image-name>
.PHONY: oci-push
oci-push: build
	@echo
	
	@echo "With the following pull command you get a kne binary at your working directory. To use this downloaded binary use - './kne <command>'\nMake sure to add ./ prefix in order to use the downloaded binary and not the globally installed kne"
	
	@echo 'If https proxy is configured in your environment, pass the proxies via --env HTTPS_PROXY="<proxy-address>" flag of the docker run command.'
	
# push to ttl.sh
	@docker run --rm -v $$(pwd):/workspace ghcr.io/oras-project/oras:v0.12.0 push ttl.sh/kne-$(COMMIT):1d ./kne > /dev/null
	@echo "download with: docker run --rm -v \$$(pwd):/workspace ghcr.io/oras-project/oras:v0.12.0 pull ttl.sh/kne-$(COMMIT):1d"