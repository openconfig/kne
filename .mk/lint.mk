
# LINTDIR is set to pwd by default, which will result in linting the whole repository
# override from command line to lint parts of repo or concrete files.
LINTDIR := $(shell pwd)
.PHONY: super-lint
super-lint:
	docker run -e RUN_LOCAL=true -e USE_FIND_ALGORITHM=true -v $(LINTDIR):/tmp/lint ghcr.io/github/super-linter:slim-v4