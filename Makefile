.PHONY: fmt lint test web-check check

fmt: ## format Go sources
	go fmt ./...

lint: ## run Go lint
	golangci-lint run ./...

test: ## run all Go tests
	go test ./...

check: ## run the complete Stage 1 gate
	bash scripts/check-toolchains.sh
	bash scripts/check-compose.sh
	$(MAKE) fmt
	$(MAKE) lint
	$(MAKE) test
