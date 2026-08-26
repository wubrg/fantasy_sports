BINS := leaguectl leaguebot leagueweb draftroom

.PHONY: build test vet fmt fmt-check lint clean check list \
	draftroom keepers board serve install refresh data-status extractor-test \
	leagueweb-install leagueweb-load leagueweb-unload leagueweb-restart leagueweb-status \
	leagueweb-serve-mount leagueweb-serve-unmount leagueweb-serve-status \
	draftroom-install draftroom-load draftroom-unload draftroom-restart draftroom-status \
	draftroom-serve-mount draftroom-serve-unmount draftroom-serve-status \
	draftroom-sam-install draftroom-sam-load draftroom-sam-unload draftroom-sam-restart draftroom-sam-status \
	draftroom-sam-serve-mount draftroom-sam-serve-unmount \
	draftroom-sam-share-mount draftroom-sam-share-unmount \
	draftroom-jeff-install draftroom-jeff-load draftroom-jeff-unload draftroom-jeff-restart draftroom-jeff-status \
	draftroom-jeff-share-mount draftroom-jeff-share-unmount

build: ## Build leaguectl, leaguebot, leagueweb, and draftroom binaries
	go build -o leaguectl ./cmd/leaguectl
	go build -o leaguebot ./cmd/leaguebot
	go build -o leagueweb ./cmd/leagueweb
	go build -o draftroom ./cmd/draftroom

# The extractors are part of the suite, so they hang off `test` rather than
# off `check`: the root Makefile forwards individual targets (fmt-check, vet,
# test) into this one rather than calling `make check` here, so anything
# reachable only from `check` never runs from the repo root.
test: ## Run go test ./... plus the Python extractor tests
	go test ./...
	$(MAKE) extractor-test

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

# Skipped only when python3 is genuinely absent — a machine with no
# interpreter cannot run them at all. A failing extractor test fails the
# build like any other.
extractor-test: ## Run the Python source-extractor tests (internal/draft/data/tools)
	@if command -v python3 >/dev/null 2>&1; then \
		cd internal/draft/data && python3 tools/test_extractors.py; \
	else \
		echo "skipping extractor tests: python3 is not installed"; \
	fi

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

calibrate: draftroom ## Ask whether spending shape predicted results (SEASONS=2023,2024,2025)
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
	@echo "==> FantasyPros ECR"
	@python3 $(TOOLS)/extract_fantasypros.py \
		$$(ls -d $(DATA)/raw/fantasypros/*/ | tail -1) \
		$(DATA)/normalized/fantasypros-2026.csv
	@echo "==> Chris Dell (expert rankings)"
	@python3 $(TOOLS)/extract_chrisdell.py \
		$$(ls -d $(DATA)/raw/chrisdell/*/ | tail -1) \
		$(DATA)/normalized/chrisdell-2026.csv
	@echo "==> DraftSheets"
	@python3 $(TOOLS)/extract_draftsheets.py \
		$$(ls -d $(DATA)/raw/draftsheets/*/ | tail -1)DraftSheets_2026.xlsx \
		$(DATA)/normalized/draftsheets-2026.csv
	@echo "==> Boris Chen (half-PPR tiers)"
# Never overwrite a file already in today's snapshot. Chen's pages are sometimes
# saved by hand instead of fetched, because the saved bundle carries
# weekly-ALL.csv — the rank dispersion, and K and DST — none of which the S3
# text files have. Re-fetching over that would replace the four skill files with
# a copy taken at a different moment, leaving them out of step with the CSV
# beside them, and the extractor's staleness check would start firing on a
# snapshot that was fine when it was saved.
	@mkdir -p $(DATA)/raw/borischen/$$(date +%F)
	@for f in text_QB.txt text_RB-HALF.txt text_WR-HALF.txt text_TE-HALF.txt; do \
		if [ -e $(DATA)/raw/borischen/$$(date +%F)/$$f ]; then \
			echo "  keeping $$f (already in today's snapshot)"; \
		else \
			curl -fsS "https://s3-us-west-1.amazonaws.com/fftiers/out/$$f" \
				-o $(DATA)/raw/borischen/$$(date +%F)/$$f || echo "  WARN: $$f fetch failed"; \
		fi; \
	done
	@python3 $(TOOLS)/extract_borischen.py \
		$$(ls -d $(DATA)/raw/borischen/*/ | tail -1) \
		$(DATA)/normalized/borischen-2026.csv
	@echo "==> generated lean sets"
	@$(MAKE) --no-print-directory leans-generate
# Last, so the manifest describes what the run just produced rather than what
# was there before it. Provenance that lags the data is worse than none: it
# reads as authoritative while pointing at the previous snapshot.
	@echo "==> provenance manifest"
	@python3 $(TOOLS)/build_manifest.py $(DATA)

data-status: ## Show how stale each source snapshot is
	@echo "private data root: $$(cd $(DATA) 2>/dev/null && pwd || echo 'NOT FOUND')"
	@echo
	@echo "raw snapshots:"
	@find $(DATA)/raw -mindepth 2 -maxdepth 2 -type d 2>/dev/null | \
		sed "s|$(DATA)/raw/||" | sort | awk '{printf "  %s\n", $$0}'
	@echo
	@echo "normalized:"
	@ls -l $(DATA)/normalized 2>/dev/null | awk 'NR>1 {printf "  %-34s %s %s %s\n", $$9, $$6, $$7, $$8}'

check: fmt-check vet test ## Run fmt-check + vet + test, extractor tests included (the pre-commit bundle)

list: ## List available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## /{printf "  %-26s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

## --- macOS-only: draftroom persistence via launchd + tailscale serve ---
## Same shape as the leagueweb targets below. The board polls Sleeper once a
## minute when no draft is running and every two seconds once one opens, so
## it is safe to leave mounted year-round rather than loaded on the night.
##
## `tailscale serve` is tailnet-only. This board carries your valuations,
## your leans and your keeper analysis, so it must never be `tailscale
## funnel`, which would put all of that on the public internet.

DRAFT_PLIST := com.draftroom.serve.plist
DRAFT_PLIST_DEST := $(HOME)/Library/LaunchAgents/$(DRAFT_PLIST)

draftroom-install: ## (macOS) Copy the draftroom plist template into ~/Library/LaunchAgents
	cp com.draftroom.serve.plist.template $(DRAFT_PLIST_DEST)
	@echo "$(DRAFT_PLIST_DEST) copied. Paths and your owner ID are pre-filled for this checkout; edit if either differs, then: make draftroom-load"

draftroom-load: ## (macOS) launchctl load the draftroom launch agent
	launchctl load $(DRAFT_PLIST_DEST)

draftroom-unload: ## (macOS) launchctl unload the draftroom launch agent
	launchctl unload $(DRAFT_PLIST_DEST)

draftroom-restart: draftroom-unload draftroom-load ## (macOS) unload then load the draftroom launch agent

draftroom-status: ## (macOS) Show whether the draftroom launch agent is loaded
	launchctl list | grep com.draftroom.serve || echo "not loaded"

draftroom-serve-mount: ## (macOS) Mount the draft board at /draftroom via tailscale serve
	tailscale serve --bg --set-path=/draftroom localhost:8083

draftroom-serve-unmount: ## (macOS) Remove the /draftroom tailscale serve mount
	tailscale serve --set-path=/draftroom off

draftroom-serve-status: ## (macOS) Show current tailscale serve mappings
	tailscale serve status

## --- macOS-only: Sam's read-only research board (/draftroom/sam, :8084) ---
## Same shape as the draftroom targets above, bound to Sam's owner id with a
## blank lean set. A second board for researching his team; not a live-draft
## board of your own.

SAM_PLIST := com.draftroom-sam.serve.plist
SAM_PLIST_DEST := $(HOME)/Library/LaunchAgents/$(SAM_PLIST)

## Guest lean sets are gitignored -- they collect someone else's reads, which
## are his and not this repo's to publish -- so a fresh checkout has only the
## tracked `blank.yaml` template. A board whose set is missing does not start,
## so install seeds it. Never clobber: an existing set holds real reads.
LEANS := internal/draft/data/leans

draftroom-sam-install: ## (macOS) Seed Sam's lean set and copy his board plist into ~/Library/LaunchAgents
	@test -f $(LEANS)/sam.yaml || cp $(LEANS)/blank.yaml $(LEANS)/sam.yaml
	cp com.draftroom-sam.serve.plist.template $(SAM_PLIST_DEST)
	@echo "$(SAM_PLIST_DEST) copied. Paths and Sam's owner ID are pre-filled for this checkout; edit if either differs, then: make draftroom-sam-load"

draftroom-sam-load: ## (macOS) launchctl load Sam's board launch agent
	launchctl load $(SAM_PLIST_DEST)

draftroom-sam-unload: ## (macOS) launchctl unload Sam's board launch agent
	launchctl unload $(SAM_PLIST_DEST)

draftroom-sam-restart: draftroom-sam-unload draftroom-sam-load ## (macOS) unload then load Sam's board launch agent

draftroom-sam-status: ## (macOS) Show whether Sam's board launch agent is loaded
	launchctl list | grep com.draftroom-sam.serve || echo "not loaded"

draftroom-sam-serve-mount: ## (macOS) Mount Sam's board at /draftroom/sam via tailscale serve
	tailscale serve --bg --set-path=/draftroom/sam localhost:8084

draftroom-sam-serve-unmount: ## (macOS) Remove the /draftroom/sam tailscale serve mount
	tailscale serve --set-path=/draftroom/sam off

## Sam reaches his board over a shared node, and Tailscale ACLs filter by port
## and never by path. Everything above is mounted on one hostname's :443, so a
## grant that reached /draftroom/sam would reach /draftroom too — your live
## board, with your real leans on it. Serving :8084 on a port of its own is what
## gives the ACL something to bind to. Still `serve`, so still tailnet-only;
## the port exists to fence a guest in, not to expose anything.

draftroom-sam-share-mount: ## (macOS) Serve Sam's board on :8443, the port his ACL grant is scoped to
	tailscale serve --bg --https=8443 localhost:8084

draftroom-sam-share-unmount: ## (macOS) Stop serving Sam's board on :8443
	tailscale serve --https=8443 off

## --- macOS-only: Jeff's board (:8086, served on :8444) ---
## Jeff drafts in his own league somewhere else, so this board is a valuation
## tool rather than a view of anyone's team here: no owner id, which leaves the
## math generic, and `-keepers none` so it opens on a full pool instead of this
## league's draft-night one. He has no Sleeper feed to follow, so he records
## each sale by hand on the board as it happens.

JEFF_PLIST := com.draftroom-jeff.serve.plist
JEFF_PLIST_DEST := $(HOME)/Library/LaunchAgents/$(JEFF_PLIST)

draftroom-jeff-install: ## (macOS) Seed Jeff's lean set and copy his board plist into ~/Library/LaunchAgents
	@test -f $(LEANS)/jeff.yaml || cp $(LEANS)/blank.yaml $(LEANS)/jeff.yaml
	cp com.draftroom-jeff.serve.plist.template $(JEFF_PLIST_DEST)
	@echo "$(JEFF_PLIST_DEST) copied. Paths are pre-filled for this checkout; edit if yours differs, then: make draftroom-jeff-load"

draftroom-jeff-load: ## (macOS) launchctl load Jeff's board launch agent
	launchctl load $(JEFF_PLIST_DEST)

draftroom-jeff-unload: ## (macOS) launchctl unload Jeff's board launch agent
	launchctl unload $(JEFF_PLIST_DEST)

draftroom-jeff-restart: draftroom-jeff-unload draftroom-jeff-load ## (macOS) unload then load Jeff's board launch agent

draftroom-jeff-status: ## (macOS) Show whether Jeff's board launch agent is loaded
	launchctl list | grep com.draftroom-jeff.serve || echo "not loaded"

draftroom-jeff-share-mount: ## (macOS) Serve Jeff's board on :8444, the port his ACL grant is scoped to
	tailscale serve --bg --https=8444 localhost:8086

draftroom-jeff-share-unmount: ## (macOS) Stop serving Jeff's board on :8444
	tailscale serve --https=8444 off

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
