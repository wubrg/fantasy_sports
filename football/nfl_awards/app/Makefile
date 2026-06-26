BINS := nflawards nflctl

.PHONY: build test vet fmt fmt-check lint clean check list \
	nflawards-install nflawards-load nflawards-unload nflawards-restart nflawards-status \
	nflawards-serve-mount nflawards-serve-unmount nflawards-serve-status

build: ## Build the nflawards web server and nflctl admin CLI
	go build -o nflawards .
	go build -o nflctl ./cmd/nflctl

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

## --- macOS-only: nflawards persistence via launchd + tailscale serve ---
## Not exercised by `check`. See README.md ("Running it persistently" /
## "Run on your desktop, reachable over Tailscale") for the full walkthrough
## these targets wrap.

PLIST := com.nflawards.serve.plist
PLIST_DEST := $(HOME)/Library/LaunchAgents/$(PLIST)

nflawards-install: ## (macOS) Copy the plist template into ~/Library/LaunchAgents
	cp com.nflawards.serve.plist.template $(PLIST_DEST)
	@echo "Edit $(PLIST_DEST) to fill in the /REPLACE/WITH/... placeholders, then run: make nflawards-load"

nflawards-load: ## (macOS) launchctl load the nflawards launch agent
	launchctl load $(PLIST_DEST)

nflawards-unload: ## (macOS) launchctl unload the nflawards launch agent
	launchctl unload $(PLIST_DEST)

nflawards-restart: nflawards-unload nflawards-load ## (macOS) unload then load the nflawards launch agent

nflawards-status: ## (macOS) Show whether the nflawards launch agent is loaded
	launchctl list | grep com.nflawards.serve || echo "not loaded"

nflawards-serve-mount: ## (macOS) Mount nflawards at /nflawards via tailscale serve
	tailscale serve --bg --set-path=/nflawards localhost:8080

nflawards-serve-unmount: ## (macOS) Remove the /nflawards tailscale serve mount
	tailscale serve --set-path=/nflawards off

nflawards-serve-status: ## (macOS) Show current tailscale serve mappings
	tailscale serve status
