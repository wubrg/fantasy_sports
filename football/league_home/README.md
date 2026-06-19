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
- `internal/core` — normalizes Sleeper data + the local history JSON into
  the operations every front end will call: `Standings`, `Faab`,
  `Matchups`, `History`, `State`.
- `cmd/leaguectl` — CLI front end, used right now to validate the core
  against the real league before building the Discord bot or web UI on
  top of the same package.
- `data/history.json` — hand-maintained award and league-role history
  (2014-2022), transcribed from `../league_fees_and_dues.md` and
  `../league_members.md`. Embedded into the binary at build time.

## Status

**Phase 1 (done):** core data layer + `leaguectl`, covering standings,
FAAB balances, weekly matchups (Sleeper-backed) and award/role history
(locally-curated).

**Not built yet:**
- Rules reference hub (render `../scoring.md`/`../rosters.md`/`../draft.md`
  live instead of duplicating them)
- League calendar / non-matchup schedule events (waivers, lineup locks,
  trade deadline — currently only in a Google Calendar, see
  `../communication.md`)
- Announcements
- Rivalries (head-to-head history across seasons — needs walking each
  season's `previous_league_id` chain and caching the result; too
  expensive to compute live on every request)
- Recap archive (revisiting later, per league discussion)
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
./leaguectl state
```

All commands default `-league` to the Hit or Miss league ID from
`../readme.md`. Everything except `history` calls the live Sleeper API, so
it needs outbound network access to `api.sleeper.app` (no auth/API key
required).

```sh
go test ./...
```

The Sleeper client and core package are tested against `httptest` fixtures
rather than the live API, so `go test` works offline.
