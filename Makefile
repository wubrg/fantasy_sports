LEAGUEHOME_DIR := league_home/app
CANTON_DIR     := canton/app

.PHONY: build test vet fmt fmt-check lint clean check list \
	leagueweb-install leagueweb-load leagueweb-unload leagueweb-restart leagueweb-status \
	leagueweb-serve-mount leagueweb-serve-unmount leagueweb-serve-status \
	canton-install canton-load canton-unload canton-restart canton-status \
	canton-serve-mount canton-serve-unmount canton-serve-status

# This repo holds two independent Go modules (league_home/app and
# canton/app), each with its own Makefile. This root Makefile
# is a delegator: every target here just forwards into both module
# Makefiles, so `make <target>` works the same from the repo root as it
# does from inside either module directory.

build: ## Build all binaries in both Go modules (leaguehome + canton)
	$(MAKE) -C $(LEAGUEHOME_DIR) build
	$(MAKE) -C $(CANTON_DIR) build

test: ## Run go test ./... in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) test
	$(MAKE) -C $(CANTON_DIR) test

vet: ## Run go vet ./... in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) vet
	$(MAKE) -C $(CANTON_DIR) vet

fmt: ## gofmt -w in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) fmt
	$(MAKE) -C $(CANTON_DIR) fmt

fmt-check: ## Fail if gofmt would reformat anything in either module
	$(MAKE) -C $(LEAGUEHOME_DIR) fmt-check
	$(MAKE) -C $(CANTON_DIR) fmt-check

lint: ## Run golangci-lint in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) lint
	$(MAKE) -C $(CANTON_DIR) lint

clean: ## Remove built binaries from both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) clean
	$(MAKE) -C $(CANTON_DIR) clean

check: fmt-check vet test ## Run fmt-check + vet + test across both modules (the pre-commit bundle)

list: ## List available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## /{printf "  %-26s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

## --- macOS-only: deployment, delegated to each app's own Makefile ---
## Not exercised by `check`. See league_home/README.md and
## canton/app/README.md for the full walkthrough these wrap.

leagueweb-install: ## (macOS) Copy the leagueweb plist template into ~/Library/LaunchAgents
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-install

leagueweb-load: ## (macOS) launchctl load the leagueweb launch agent
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-load

leagueweb-unload: ## (macOS) launchctl unload the leagueweb launch agent
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-unload

leagueweb-restart: ## (macOS) unload then load the leagueweb launch agent
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-restart

leagueweb-status: ## (macOS) Show whether the leagueweb launch agent is loaded
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-status

leagueweb-serve-mount: ## (macOS) Mount leagueweb at /leagueweb via tailscale serve
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-serve-mount

leagueweb-serve-unmount: ## (macOS) Remove the /leagueweb tailscale serve mount
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-serve-unmount

leagueweb-serve-status: ## (macOS) Show current tailscale serve mappings
	$(MAKE) -C $(LEAGUEHOME_DIR) leagueweb-serve-status

canton-install: ## (macOS) Copy the canton plist template into ~/Library/LaunchAgents
	$(MAKE) -C $(CANTON_DIR) canton-install

canton-load: ## (macOS) launchctl load the canton launch agent
	$(MAKE) -C $(CANTON_DIR) canton-load

canton-unload: ## (macOS) launchctl unload the canton launch agent
	$(MAKE) -C $(CANTON_DIR) canton-unload

canton-restart: ## (macOS) unload then load the canton launch agent
	$(MAKE) -C $(CANTON_DIR) canton-restart

canton-status: ## (macOS) Show whether the canton launch agent is loaded
	$(MAKE) -C $(CANTON_DIR) canton-status

canton-serve-mount: ## (macOS) Mount canton at /canton via tailscale serve
	$(MAKE) -C $(CANTON_DIR) canton-serve-mount

canton-serve-unmount: ## (macOS) Remove the /canton tailscale serve mount
	$(MAKE) -C $(CANTON_DIR) canton-serve-unmount

canton-serve-status: ## (macOS) Show current tailscale serve mappings
	$(MAKE) -C $(CANTON_DIR) canton-serve-status
