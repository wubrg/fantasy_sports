APP    := app
MODEL  := model
PY     := python3
DEMO_LOG := /tmp/edgectl-demo.jsonl

.PHONY: help build check test vet fmt fmt-check lint clean \
	board board-check board-report \
	data data-status data-clean \
	fit fit-residuals fit-conditionals verify-model lag findings \
	demo demo-clean

# This is the working entry point for the edge app. It covers both halves:
# the Go module under app/ (which the ROOT Makefile also builds, alongside the
# other two modules) and the Python model pipeline under model/, which the root
# knows nothing about.
#
# The split is deliberate. Python fits; Go serves. The boundary is a committed
# JSON artifact read via go:embed, so edgectl has no runtime data dependency and
# a model refresh is a data change reviewed in a diff.

help: ## Show this help
	@echo "edge — wagering math and the frameworks it enforces"
	@echo
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## /{printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo
	@echo "  vet fmt fmt-check lint clean   also forwarded to app/"
	@echo
	@echo "Typical first run:"
	@echo "  make data     # ~500 MB of open NFL data, once"
	@echo "  make check    # Go tests"
	@echo "  make demo     # worked examples, no data needed"

## --- Go (delegated to app/; the root Makefile builds this too) ---

build: ## Build edgectl
	$(MAKE) -C $(APP) build

check: ## fmt-check + vet + test (the pre-commit bundle)
	$(MAKE) -C $(APP) check

test: ## Run go test ./...
	$(MAKE) -C $(APP) test

vet fmt fmt-check lint clean:
	$(MAKE) -C $(APP) $@

## --- Line board ---

board: ## Scaffold the per-week line board from the schedule (never overwrites a price)
	@$(MAKE) -C $(APP) build
	@cd $(APP) && ./edgectl board scaffold

board-check: ## Parse every board cell and report the malformed ones
	@cd $(APP) && ./edgectl board validate

# WEEK and BOOK are the only knobs worth a Make variable; everything else
# (stake, shots, target) is a per-run decision and belongs on the command line.
board-report: ## Analyse one week: make board-report WEEK=1 [BOOK=consensus]
	@$(MAKE) -C $(APP) build
	@cd $(APP) && ./edgectl board report -week $(WEEK) -book $(or $(BOOK),consensus)

## --- macOS-only: the board as a launchd agent + tailscale serve ---
##
## `tailscale serve` is tailnet-only. This board carries your wagering
## positions and the prices you took; it must NEVER be `tailscale funnel`,
## which would publish all of it to the public internet.
##
## One-time setup (plist placeholders, path-mount caveats) follows the same
## shape as leagueweb and draftroom -- see league_home/README.md.

EDGE_PLIST := com.edge.serve.plist
EDGE_PLIST_DEST := $(HOME)/Library/LaunchAgents/$(EDGE_PLIST)

board-install: ## (macOS) Copy the board plist template into ~/Library/LaunchAgents
	cp $(APP)/com.edge.serve.plist.template $(EDGE_PLIST_DEST)
	@echo "$(EDGE_PLIST_DEST) copied. Paths are pre-filled for this checkout; edit if yours differs, then: make board-load"

board-load: ## (macOS) launchctl load the board launch agent
	launchctl load $(EDGE_PLIST_DEST)

board-unload: ## (macOS) launchctl unload the board launch agent
	launchctl unload $(EDGE_PLIST_DEST)

board-restart: board-unload board-load ## (macOS) unload then load the board launch agent

board-status: ## (macOS) Show whether the board launch agent is loaded
	launchctl list | grep com.edge.serve || echo "not loaded"

board-serve-mount: ## (macOS) Mount the line board at /edge via tailscale serve
	tailscale serve --bg --set-path=/edge localhost:8085

board-serve-unmount: ## (macOS) Remove the /edge tailscale serve mount
	tailscale serve --set-path=/edge off

board-serve-status: ## (macOS) Show current tailscale serve mappings
	tailscale serve status

## --- Data (open nflverse files; network, and it is large) ---

data: ## Fetch the nflverse cache (~500 MB, idempotent, gitignored)
	$(PY) $(MODEL)/ingest/nflverse.py

data-status: ## Show what is cached without fetching
	@$(PY) $(MODEL)/ingest/nflverse.py --dry-run

data-clean: ## Delete the cache (regenerable with `make data`)
	rm -rf $(MODEL)/data/raw

## --- Model ---
##
## The fit targets REWRITE COMMITTED ARTIFACTS under
## app/internal/scenario/artifacts/. Run them deliberately, and read the diff:
## a refit changes what edgectl prices. Use verify-model to check the numbers
## without touching anything.

verify-model: ## Re-run both fits read-only and print calibration (writes nothing)
	@echo "== residuals: held-out calibration =="
	@$(PY) $(MODEL)/analysis/fit_residuals.py --validate-only
	@echo
	@echo "== conditionals: scenario separation =="
	@$(PY) $(MODEL)/analysis/fit_conditionals.py --report

fit: fit-residuals fit-conditionals ## Regenerate BOTH artifacts (rewrites committed files)

fit-residuals: ## Refit the game-line residual distribution
	$(PY) $(MODEL)/analysis/fit_residuals.py

fit-conditionals: ## Refit the pooled q/r grid
	$(PY) $(MODEL)/analysis/fit_conditionals.py

lag: ## Run the utilization-lag test (~10s)
	$(PY) $(MODEL)/analysis/utilization_lag.py

compare-oos: ## Reproduce the rejected magnitude-aware out-of-sample table (FINDINGS.md 4)
	@cd $(MODEL)/analysis && $(PY) validate.py --compare-oos

findings: verify-model lag compare-oos ## Reproduce every measured claim in model/FINDINGS.md

## --- Examples ---

demo: build demo-clean ## Walk through every command with real numbers
	@./$(APP)/edgectl card bonus
	@echo
	@echo "================================================================"
	@echo " A player's game log, and what price it can support"
	@echo "================================================================"
	@./$(APP)/edgectl hitrate -label "Player A — receiving yards" \
		-line 52.5 -side over \
		-values "61,58,55,72,39,58,44,66,71,49,63,57,80,41,54,68,45" \
		-price -110
	@echo
	@echo "================================================================"
	@echo " The same wager as a belief, priced off the game line"
	@echo "================================================================"
	@./$(APP)/edgectl scenario -name shootout -total 49 -threshold 50 \
		-belief 0.55 -targets 7.5 -trend 0.07 -line 52.5 \
		-price 100 -stake 3 -label "Player A over 52.5 rec yds" -log $(DEMO_LOG)
	@echo
	@echo "================================================================"
	@echo " An alternate-line ladder: which rung needs the smallest leap"
	@echo "================================================================"
	@echo " -rungs takes YOUR conditionals, not the grid's. The q and r below"
	@echo " are the fitted values for this cell at each line, so the demo shows"
	@echo " what the tool measures rather than a number chosen to look good."
	@echo ""
	@echo " Every rung comes back beyond-your-read, and that is the honest"
	@echo " result: the grid pools every player in the band, so a line set at"
	@echo " one player's own median sits above the cohort's. See"
	@echo " docs/reviews/2026-08-23-adversarial.md, finding C3."
	@./$(APP)/edgectl scenario -name shootout -total 49 -threshold 50 -belief 0.55 \
		-bankroll bonus -stake 10 \
		-rungs "52.5:-110:0.550:0.435,75.5:145:0.342:0.226,100.5:450:0.192:0.090"
	@echo
	@echo "================================================================"
	@echo " Settle it, then score your calibration"
	@echo "================================================================"
	@id=$$(./$(APP)/edgectl log list -file $(DEMO_LOG) | awk 'NR>2 && NF{print $$1; exit}'); \
		./$(APP)/edgectl log settle -file $(DEMO_LOG) -id "$$id" -result won -note "cleared by 12"
	@./$(APP)/edgectl log score -file $(DEMO_LOG)
	@echo
	@echo "demo log: $(DEMO_LOG)   (make demo-clean to remove)"

demo-clean: ## Remove the demo bet log
	@rm -f $(DEMO_LOG)
