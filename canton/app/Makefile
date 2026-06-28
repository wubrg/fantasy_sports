BINS := canton cantonctl

.PHONY: build test vet fmt fmt-check lint clean check list \
	canton-install canton-load canton-unload canton-restart canton-status \
	canton-serve-mount canton-serve-unmount canton-serve-status

build: ## Build the canton web server and cantonctl admin CLI
	go build -o canton .
	go build -o cantonctl ./cmd/cantonctl

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

## --- macOS-only: canton persistence via launchd + tailscale serve ---
## Not exercised by `check`. See README.md ("Running it persistently" /
## "Run on your desktop, reachable over Tailscale") for the full walkthrough
## these targets wrap.

PLIST := com.canton.serve.plist
PLIST_DEST := $(HOME)/Library/LaunchAgents/$(PLIST)

canton-install: ## (macOS) Copy the plist template into ~/Library/LaunchAgents
	cp com.canton.serve.plist.template $(PLIST_DEST)
	@echo "Edit $(PLIST_DEST) to fill in the /REPLACE/WITH/... placeholders, then run: make canton-load"

canton-load: ## (macOS) launchctl load the canton launch agent
	launchctl load $(PLIST_DEST)

canton-unload: ## (macOS) launchctl unload the canton launch agent
	launchctl unload $(PLIST_DEST)

canton-restart: canton-unload canton-load ## (macOS) unload then load the canton launch agent

canton-status: ## (macOS) Show whether the canton launch agent is loaded
	launchctl list | grep com.canton.serve || echo "not loaded"

canton-serve-mount: ## (macOS) Mount canton at /canton via tailscale serve
	tailscale serve --bg --set-path=/canton localhost:8080

canton-serve-unmount: ## (macOS) Remove the /canton tailscale serve mount
	tailscale serve --set-path=/canton off

canton-serve-status: ## (macOS) Show current tailscale serve mappings
	tailscale serve status
