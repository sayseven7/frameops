.PHONY: fmt lint test web-check github-actions-check check

export DOCKER_CONFIG ?= $(shell getent passwd "$$(id -u)" | cut -d: -f6)/.docker

fmt: ## format Go sources
	go fmt ./...

lint: ## run Go lint
	golangci-lint run ./...

test: ## run all Go tests
	go test ./...
	bash scripts/migrations-contract_test.sh
	bash scripts/check-compose_test.sh
	bash scripts/local-runtime_test.sh
	bash scripts/recovery_test.sh
	bash scripts/deploy-contract_test.sh

web-check: ## verify the frozen frontend workspace
	pnpm install --frozen-lockfile --ignore-scripts
	pnpm --filter @frameops/web test
	pnpm --filter @frameops/web lint
	pnpm --filter @frameops/web typecheck
	FRAMEOPS_API_URL=http://127.0.0.1:8081 pnpm --filter @frameops/web build

github-actions-check: ## verify GitHub Actions security contracts
	bash scripts/check_github_actions_test.sh

check: ## run the complete Stage 1 gate
	bash scripts/check-toolchains.sh
	bash scripts/check-compose.sh
	$(MAKE) github-actions-check
	$(MAKE) fmt
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) web-check
