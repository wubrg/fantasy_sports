# fantasy_sports

Tools and reference data for fantasy football leagues, plus a related NFL
historical reference app.

- `leagues/` — per-league rules, governance docs, and history.
  `leagues/hit_or_miss/` is the active league; `leagues/family_football/` is
  a placeholder for a second league not yet set up.
- `league_home/` — Go apps (`leaguectl` CLI, `leaguebot` Discord bot,
  `leagueweb` web UI) that pull live standings/matchups/FAAB from Sleeper
  plus the locally-curated league history. See `league_home/README.md`.
- `canton/` — NFL awards/Pro Bowl/All-Pro historical reference app
  (`canton` web server + `cantonctl` admin CLI), independent of any one
  league. See `canton/README.md`.
- `archive/` — retired Ruby and Python tooling, kept for reference only;
  see each subfolder for why it's no longer in active use.

Each app lives in its own Go module with its own README and Makefile; see
`CONTRIBUTING.md` for how the root `Makefile` ties them together.
