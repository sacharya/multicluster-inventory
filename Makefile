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

.PHONY: sanity build test help
