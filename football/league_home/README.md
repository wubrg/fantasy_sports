# League Home

A planned home base for the `Hit or Miss` league (see `../readme.md`):
live standings/matchups/FAAB pulled from Sleeper, plus the league's award
and governance history that predates Sleeper and isn't in its API.

## Design

One core data layer, two thin front ends planned on top of it — neither
front end owns league-data logic, they just call the same operations and
format the result differently:

```
        Sleeper API ─┐
                      ├─▶  core (internal/core)  ◀── data/history.json
   data/history.json ─┘            │
                       ┌────────────┴────────────┐
                  leaguectl                  (planned)
                  (CLI, this phase)      Discord bot · Web UI
```

- `internal/sleeper` — minimal client for the public, keyless Sleeper API
  (league settings, rosters, users, matchups, NFL state).
- `internal/core` — normalizes Sleeper data + the local JSON into the
  operations every front end will call: `Standings`, `Faab`, `Matchups`,
  `History`, `Rules`, `State`.
- `cmd/leaguectl` — CLI front end, used right now to validate the core
  against the real league before building the Discord bot or web UI on
  top of the same package.
- `data/history.json` — hand-maintained award and league-role history
  (2014-2022), transcribed from `../league_fees_and_dues.md` and
  `../league_members.md`. Embedded into the binary at build time.
- `data/rules.json` — current-season ruleset (roster slots, keepers,
  waivers, draft, trade deadline, playoffs, governance), transcribed from
  `../rosters.md`, `../draft.md`, `../league_fees_and_dues.md` and
  `../policies_and_procedures.md`. Holds today's rules only, not a
  history of past changes. Embedded into the binary at build time.

## Status

**Phase 1 (done):** core data layer + `leaguectl`, covering standings,
FAAB balances, weekly matchups (Sleeper-backed) and award/role history
(locally-curated).

**Phase 2 (done):** rules data layer, covering roster slots, keepers,
waivers, draft format, trade deadline, playoffs and governance
(locally-curated, current season only). Scoring is deliberately excluded:
it lives in Sleeper's own `league.scoring_settings`, the actual source of
truth points get computed from, so hand-transcribing it here risks
silently drifting out of sync (as `../scoring.md`'s own half-PPR ambiguity
already shows). It'll be added later as a live Sleeper-backed lookup
instead.

**Not built yet:**
- Scoring (live from Sleeper's `league.scoring_settings`, not
  hand-transcribed)
- Manager identity (a stable entity to join Sleeper accounts, history and
  rivalries to the same real person across seasons)
- League calendar / non-matchup schedule events (waivers, lineup locks,
  trade deadline — currently only in a Google Calendar, see
  `../communication.md`)
- Announcements
- Rivalries (head-to-head history across seasons — needs walking each
  season's `previous_league_id` chain and caching the result; too
  expensive to compute live on every request)
- Recap archive (revisiting later, per league discussion)
- Side pots (revisiting later, per league discussion)
- Discord bot front end
- Web UI front end

## Running it

```sh
cd football/league_home/app
go build -o leaguectl ./cmd/leaguectl
./leaguectl standings
./leaguectl faab
./leaguectl matchups -week 7
./leaguectl history
./leaguectl rules
./leaguectl state
```

All commands default `-league` to the Hit or Miss league ID from
`../readme.md`. Everything except `history` and `rules` calls the live
Sleeper API, so it needs outbound network access to `api.sleeper.app` (no
auth/API key required).

```sh
go test ./...
```

The Sleeper client and core package are tested against `httptest` fixtures
rather than the live API, so `go test` works offline.
