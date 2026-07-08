BINS := leaguectl leaguebot leagueweb

.PHONY: build test vet fmt fmt-check lint clean check list \
	leagueweb-install leagueweb-load leagueweb-unload leagueweb-restart leagueweb-status \
	leagueweb-serve-mount leagueweb-serve-unmount leagueweb-serve-status

build: ## Build leaguectl, leaguebot, and leagueweb binaries
	go build -o leaguectl ./cmd/leaguectl
	go build -o leaguebot ./cmd/leaguebot
	go build -o leagueweb ./cmd/leagueweb

test: ## Run go test ./...
	go test ./...

vet: ## Run go vet ./...
	go vet ./...

fmt: ## Reformat all Go source with gofmt -w
	gofmt -w .

fmt-check: ## Fail if any file needs gofmt (no writes)
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	fi

lint: ## Run golangci-lint
	golangci-lint run ./...

clean: ## Remove built binaries
	rm -f $(BINS)

check: fmt-check vet test ## Run fmt-check + vet + test (the pre-commit bundle)

list: ## List available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## /{printf "  %-26s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

## --- macOS-only: leagueweb persistence via launchd + tailscale serve ---
## Not exercised by `check`. See ../../league_home/README.md ("Running it
## persistently" / "Run on your desktop, reachable over Tailscale") for the
## full walkthrough these targets wrap.

PLIST := com.leagueweb.serve.plist
PLIST_DEST := $(HOME)/Library/LaunchAgents/$(PLIST)

leagueweb-install: ## (macOS) Copy the plist template into ~/Library/LaunchAgents
	cp com.leagueweb.serve.plist.template $(PLIST_DEST)
	@echo "Edit $(PLIST_DEST) to fill in the /REPLACE/WITH/... placeholders, then run: make leagueweb-load"

leagueweb-load: ## (macOS) launchctl load the leagueweb launch agent
	launchctl load $(PLIST_DEST)

leagueweb-unload: ## (macOS) launchctl unload the leagueweb launch agent
	launchctl unload $(PLIST_DEST)

leagueweb-restart: leagueweb-unload leagueweb-load ## (macOS) unload then load the leagueweb launch agent

leagueweb-status: ## (macOS) Show whether the leagueweb launch agent is loaded
	launchctl list | grep com.leagueweb.serve || echo "not loaded"

leagueweb-serve-mount: ## (macOS) Mount leagueweb at /leagueweb via tailscale serve
	tailscale serve --bg --set-path=/leagueweb localhost:8081

leagueweb-serve-unmount: ## (macOS) Remove the /leagueweb tailscale serve mount
	tailscale serve --set-path=/leagueweb off

leagueweb-serve-status: ## (macOS) Show current tailscale serve mappings
	tailscale serve status
