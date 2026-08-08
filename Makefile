LEAGUEHOME_DIR := league_home/app
CANTON_DIR     := canton/app
EDGE_DIR       := edge/app

.PHONY: build test vet fmt fmt-check lint clean check list \
	leagueweb-install leagueweb-load leagueweb-unload leagueweb-restart leagueweb-status \
	leagueweb-serve-mount leagueweb-serve-unmount leagueweb-serve-status \
	canton-install canton-load canton-unload canton-restart canton-status \
	canton-serve-mount canton-serve-unmount canton-serve-status \
	draftroom-install draftroom-load draftroom-unload draftroom-restart draftroom-status \
	draftroom-serve-mount draftroom-serve-unmount draftroom-serve-status

# This repo holds three independent Go modules (league_home/app,
# canton/app and edge/app), each with its own Makefile. This root Makefile
# is a delegator: every target here just forwards into all three module
# Makefiles, so `make <target>` works the same from the repo root as it
# does from inside any module directory.

build: ## Build all binaries in all three Go modules (leaguehome + canton + edge)
	$(MAKE) -C $(LEAGUEHOME_DIR) build
	$(MAKE) -C $(CANTON_DIR) build
	$(MAKE) -C $(EDGE_DIR) build

test: ## Run go test ./... in all three Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) test
	$(MAKE) -C $(CANTON_DIR) test
	$(MAKE) -C $(EDGE_DIR) test

vet: ## Run go vet ./... in all three Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) vet
	$(MAKE) -C $(CANTON_DIR) vet
	$(MAKE) -C $(EDGE_DIR) vet

fmt: ## gofmt -w in all three Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) fmt
	$(MAKE) -C $(CANTON_DIR) fmt
	$(MAKE) -C $(EDGE_DIR) fmt

fmt-check: ## Fail if gofmt would reformat anything in any module
	$(MAKE) -C $(LEAGUEHOME_DIR) fmt-check
	$(MAKE) -C $(CANTON_DIR) fmt-check
	$(MAKE) -C $(EDGE_DIR) fmt-check

lint: ## Run golangci-lint in all three Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) lint
	$(MAKE) -C $(CANTON_DIR) lint
	$(MAKE) -C $(EDGE_DIR) lint

clean: ## Remove built binaries from all three Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) clean
	$(MAKE) -C $(CANTON_DIR) clean
	$(MAKE) -C $(EDGE_DIR) clean

check: fmt-check vet test ## Run fmt-check + vet + test across all three modules (the pre-commit bundle)

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

draftroom-install: ## (macOS) Copy the draftroom plist template into ~/Library/LaunchAgents
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-install

draftroom-load: ## (macOS) launchctl load the draftroom launch agent
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-load

draftroom-unload: ## (macOS) launchctl unload the draftroom launch agent
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-unload

draftroom-restart: ## (macOS) unload then load the draftroom launch agent
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-restart

draftroom-status: ## (macOS) Show whether the draftroom launch agent is loaded
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-status

draftroom-serve-mount: ## (macOS) Mount the draft board at /draftroom via tailscale serve
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-serve-mount

draftroom-serve-unmount: ## (macOS) Remove the /draftroom tailscale serve mount
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-serve-unmount

draftroom-serve-status: ## (macOS) Show current tailscale serve mappings
	$(MAKE) -C $(LEAGUEHOME_DIR) draftroom-serve-status

canton-serve-status: ## (macOS) Show current tailscale serve mappings
	$(MAKE) -C $(CANTON_DIR) canton-serve-status
