LEAGUEHOME_DIR := football/league_home/app
NFLAWARDS_DIR  := football/nfl_awards/app

.PHONY: build test vet fmt fmt-check lint clean check list \
	leagueweb-install leagueweb-load leagueweb-unload leagueweb-restart leagueweb-status \
	leagueweb-serve-mount leagueweb-serve-unmount leagueweb-serve-status \
	nflawards-install nflawards-load nflawards-unload nflawards-restart nflawards-status \
	nflawards-serve-mount nflawards-serve-unmount nflawards-serve-status

# This repo holds two independent Go modules (football/league_home/app and
# football/nfl_awards/app), each with its own Makefile. This root Makefile
# is a delegator: every target here just forwards into both module
# Makefiles, so `make <target>` works the same from the repo root as it
# does from inside either module directory.

build: ## Build all binaries in both Go modules (leaguehome + nflawards)
	$(MAKE) -C $(LEAGUEHOME_DIR) build
	$(MAKE) -C $(NFLAWARDS_DIR) build

test: ## Run go test ./... in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) test
	$(MAKE) -C $(NFLAWARDS_DIR) test

vet: ## Run go vet ./... in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) vet
	$(MAKE) -C $(NFLAWARDS_DIR) vet

fmt: ## gofmt -w in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) fmt
	$(MAKE) -C $(NFLAWARDS_DIR) fmt

fmt-check: ## Fail if gofmt would reformat anything in either module
	$(MAKE) -C $(LEAGUEHOME_DIR) fmt-check
	$(MAKE) -C $(NFLAWARDS_DIR) fmt-check

lint: ## Run golangci-lint in both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) lint
	$(MAKE) -C $(NFLAWARDS_DIR) lint

clean: ## Remove built binaries from both Go modules
	$(MAKE) -C $(LEAGUEHOME_DIR) clean
	$(MAKE) -C $(NFLAWARDS_DIR) clean

check: fmt-check vet test ## Run fmt-check + vet + test across both modules (the pre-commit bundle)

list: ## List available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## /{printf "  %-26s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

## --- macOS-only: deployment, delegated to each app's own Makefile ---
## Not exercised by `check`. See football/league_home/README.md and
## football/nfl_awards/app/README.md for the full walkthrough these wrap.

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

nflawards-install: ## (macOS) Copy the nflawards plist template into ~/Library/LaunchAgents
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-install

nflawards-load: ## (macOS) launchctl load the nflawards launch agent
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-load

nflawards-unload: ## (macOS) launchctl unload the nflawards launch agent
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-unload

nflawards-restart: ## (macOS) unload then load the nflawards launch agent
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-restart

nflawards-status: ## (macOS) Show whether the nflawards launch agent is loaded
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-status

nflawards-serve-mount: ## (macOS) Mount nflawards at /nflawards via tailscale serve
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-serve-mount

nflawards-serve-unmount: ## (macOS) Remove the /nflawards tailscale serve mount
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-serve-unmount

nflawards-serve-status: ## (macOS) Show current tailscale serve mappings
	$(MAKE) -C $(NFLAWARDS_DIR) nflawards-serve-status
