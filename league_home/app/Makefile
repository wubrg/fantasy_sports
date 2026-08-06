BINS := leaguectl leaguebot leagueweb draftroom

.PHONY: build test vet fmt fmt-check lint clean check list \
	draftroom keepers board serve install refresh data-status extractor-test \
	leagueweb-install leagueweb-load leagueweb-unload leagueweb-restart leagueweb-status \
	leagueweb-serve-mount leagueweb-serve-unmount leagueweb-serve-status

build: ## Build leaguectl, leaguebot, leagueweb, and draftroom binaries
	go build -o leaguectl ./cmd/leaguectl
	go build -o leaguebot ./cmd/leaguebot
	go build -o leagueweb ./cmd/leagueweb
	go build -o draftroom ./cmd/draftroom

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

extractor-test: ## Run the Python source-extractor tests (internal/draft/data/tools)
	@cd internal/draft/data && python3 tools/test_extractors.py

## --- draft room ---------------------------------------------------------
## The private data repo holds every proprietary source. Override its
## location with DATA=/path/to/fantasy_sports_data or $DRAFTROOM_DATA_DIR.

DATA ?= ../../../fantasy_sports_data
TOOLS := internal/draft/data/tools
ME ?= $(DRAFTROOM_OWNER_ID)

# LEANS is the opinion sets to apply, in precedence order: the first set to
# name a player owns him. Your own reads alone by default.
LEANS ?= mine

draftroom: ## Build just the draftroom binary
	go build -o draftroom ./cmd/draftroom

keepers: draftroom ## Keeper reconciliation, true budgets, and 2026 keeper prices
	./draftroom keepers

board: draftroom ## The draft board (BASELINE=beer|beerplus|vols, ME=<owner id>, LIMIT=n, LEANS=mine,menton)
	./draftroom board $(if $(ME),-me $(ME),) $(if $(BASELINE),-baseline $(BASELINE),) $(if $(LIMIT),-limit $(LIMIT),) -leans $(LEANS)

serve: draftroom ## Serve the board as a web page (ME=<owner id>, ADDR=:8083, LEANS=mine,menton)
	./draftroom serve $(if $(ME),-me $(ME),) $(if $(ADDR),-addr $(ADDR),) $(if $(BASELINE),-baseline $(BASELINE),) -leans $(LEANS)

leans: draftroom ## Show the merged lean sets and what they disagree about (LEANS=mine,menton)
	./draftroom leans -leans $(LEANS)

leans-generate: draftroom ## Rebuild the generated lean sets from the private source data
	./draftroom leans -generate

calibrate: draftroom ## Measure the archetype thresholds against past drafts (SEASONS=2023,2024,2025)
	./draftroom calibrate $(if $(SEASONS),-seasons "$(SEASONS)",)

install: ## Install draftroom into $GOBIN so it runs from any directory
	@go install -ldflags "\
		-X main.builtinConfigDir=$$(pwd)/internal/draft/data \
		-X main.builtinDataDir=$$(cd $(DATA) && pwd)" ./cmd/draftroom
	@echo "installed to $$(go env GOPATH)/bin/draftroom"
	@echo "  config: $$(pwd)/internal/draft/data"
	@echo "  data:   $$(cd $(DATA) && pwd)"
	@echo "ensure $$(go env GOPATH)/bin is on your PATH; no env vars needed"

refresh: ## Re-extract every source from the private data repo and rebuild generated lean sets
	@echo "==> Ciely"
	@python3 $(TOOLS)/extract_ciely.py \
		$$(ls -d $(DATA)/raw/ciely/*/ | tail -1)2026FFBProjections.xlsx \
		$(DATA)/normalized/ciely-2026.csv
	@echo "==> Subvertadown"
	@python3 $(TOOLS)/extract_subvertadown.py \
		$$(ls -d $(DATA)/raw/subvertadown/*/ | tail -1)sheets \
		$(DATA)/normalized/subvertadown-2026.csv
	@echo "==> Fantasy Points (Big 3)"
	@python3 $(TOOLS)/extract_fantasypoints.py \
		"$$(ls $(DATA)/raw/fantasypoints/*.html | tail -1)" \
		$(DATA)/normalized/fantasypoints-big3-2026.csv
	@echo "==> generated lean sets"
	@$(MAKE) --no-print-directory leans-generate

data-status: ## Show how stale each source snapshot is
	@echo "private data root: $$(cd $(DATA) 2>/dev/null && pwd || echo 'NOT FOUND')"
	@echo
	@echo "raw snapshots:"
	@find $(DATA)/raw -mindepth 2 -maxdepth 2 -type d 2>/dev/null | \
		sed "s|$(DATA)/raw/||" | sort | awk '{printf "  %s\n", $$0}'
	@echo
	@echo "normalized:"
	@ls -l $(DATA)/normalized 2>/dev/null | awk 'NR>1 {printf "  %-34s %s %s %s\n", $$9, $$6, $$7, $$8}'

check: fmt-check vet test extractor-test ## Run fmt-check + vet + test + extractor tests (the pre-commit bundle)

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
	@echo "$(PLIST_DEST) copied. Paths in the template are pre-filled for this checkout; edit if your checkout lives elsewhere, then run: make leagueweb-load"

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
