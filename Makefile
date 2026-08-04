.PHONY: fmt lint test web-check github-actions-check check

fmt: ## format Go sources
	go fmt ./...

lint: ## run Go lint
	golangci-lint run ./...

test: ## run all Go tests
	go test ./...

web-check: ## verify the frozen frontend workspace
	pnpm install --frozen-lockfile --ignore-scripts
	pnpm --filter @frameops/web lint
	pnpm --filter @frameops/web typecheck
	pnpm --filter @frameops/web build

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
