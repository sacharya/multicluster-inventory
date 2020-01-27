IMAGE_REGISTRY ?= "quay.io"
REGISTRY_NAMESPACE ?= "mcmdev"
IMAGE_TAG ?= "latest"

# Use podman if available, otherwise use docker
ifeq ($(IMAGE_BUILD_CMD),)
	IMAGE_BUILD_CMD = $(shell podman version > /dev/null && echo podman || echo docker)
endif

sanity: ## Check the sanity of the project
	go mod tidy
	go vet ./...
	go fmt ./...
	git diff --exit-code

build: ## Build binary from source
	go build -i ./cmd/manager/main.go

test: ## Unit test the project
	go test -v ./...

help: ## Show this help screen
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@grep -E '^[ a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ''

build-image: build ## Build the operator image
	$(IMAGE_BUILD_CMD) build -f build/Dockerfile -t $(IMAGE_REGISTRY)/$(REGISTRY_NAMESPACE)/multicluster-inventory:$(IMAGE_TAG) .

.PHONY: sanity build build-image test help
