BINS := edgectl

.PHONY: build test vet fmt fmt-check lint clean check list

build: ## Build the edgectl wager-math CLI
	go build -o edgectl ./cmd/edgectl

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

## The board's UI is JavaScript, which `go test` cannot see. `node --check`
## only parses -- it passes happily on a file with a function deleted out of
## it, which then fails at render time while the page still serves HTTP 200.
## smoke.js executes the thing. Skipped when node is absent rather than
## failing the build, since the Go side must stay buildable without it.
smoke: ## Execute the board UI against a stub DOM (needs node; skipped if absent)
	@if command -v node >/dev/null 2>&1; then \
		node cmd/edgectl/static/smoke.js; \
	else \
		echo "node not found - skipping the board UI smoke test"; \
	fi

check: fmt-check vet test smoke ## Run fmt-check + vet + test + UI smoke (the pre-commit bundle)

list: ## List available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## /{printf "  %-26s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort
