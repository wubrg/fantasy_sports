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
                  leaguectl                  leaguebot        (planned)
                  (CLI, Phase 1)          (Discord, Phase 4)     Web UI
```

- `internal/sleeper` — minimal client for the public, keyless Sleeper API
  (league settings, rosters, users, matchups, NFL state).
- `internal/core` — normalizes Sleeper data + the local JSON into the
  operations every front end will call: `Standings`, `Faab`, `Matchups`,
  `History`, `Rules`, `Scoring`, `Managers`, `Announcements`, `Schedule`,
  `Rivalries`, `State`.
- `cmd/leaguectl` — CLI front end, used right now to validate the core
  against the real league before building the Discord bot or web UI on
  top of the same package.
- `cmd/leaguebot` — Discord bot front end. Exposes the same operations as
  leaguectl, one per slash command, against a single league configured at
  startup (no per-command `-league` override, unlike the CLI, since one
  bot install only ever serves the one server/league it's invited to).
- `data/history.json` — hand-maintained award and league-role history
  (2014-2022), transcribed from `../league_fees_and_dues.md` and
  `../league_members.md`. Embedded into the binary at build time.
- `data/rules.json` — current-season ruleset (roster slots, keepers,
  waivers, draft, trade deadline, playoffs, governance), transcribed from
  `../rosters.md`, `../draft.md`, `../league_fees_and_dues.md` and
  `../policies_and_procedures.md`. Holds today's rules only, not a
  history of past changes. Embedded into the binary at build time.
- `data/managers.json` — every manager who's ever owned a team, past and
  present, with name-spelling aliases (e.g. "Chris Bushjost" /
  "Chris Buschjost") so history and Sleeper data can be joined to one
  stable identity regardless of which spelling/season used. Transcribed
  from `../league_members.md` plus the variants in `data/history.json`.
- `data/announcements.json`, `data/schedule.json`, `data/rivalries.json`
  — see Status below; these are placeholder/example data or intentionally
  empty pending a real source.

## Status

**Phase 1 (done):** core data layer + `leaguectl`, covering standings,
FAAB balances, weekly matchups (Sleeper-backed) and award/role history
(locally-curated).

**Phase 2 (done):** rules data layer, covering roster slots, keepers,
waivers, draft format, trade deadline, playoffs and governance
(locally-curated, current season only). Scoring was deliberately excluded
at the time: it lives in Sleeper's own `league.scoring_settings`, the
actual source of truth points get computed from, so hand-transcribing it
risked silently drifting out of sync (as `../scoring.md`'s own half-PPR
ambiguity already showed). `Scoring()` now fills that gap live — see
Phase 5 below.

**Phase 3 (done, schema + placeholder/mock data):** `Managers`,
`Announcements`, `Schedule`, `Rivalries`. `Managers` is real, curated data
(see `data/managers.json` above). The other three ship with the schema
and a working `leaguectl` command, but not real data yet, since each
needs something this environment doesn't have:
- `Announcements` — example entries only; there's no real feed to
  transcribe until a way to write to it (a Discord-reading bot, or a
  small posting tool) exists.
- `Schedule` — illustrative entries derived from `Rules` (trade deadline,
  playoff weeks) plus the known recurring/structural events; real
  calendar dates still live only in the Google Calendar from
  `../communication.md`.
- `Rivalries` — ships an intentionally empty dataset. Real head-to-head
  records need walking each season's `previous_league_id` chain back
  through Sleeper and aggregating every matchup, which needs live Sleeper
  API access this sandboxed environment doesn't have. Fabricating
  win/loss numbers for real people instead would just be wrong, so the
  schema exists and the data waits for a real sync job.

**Phase 4 (done):** `cmd/leaguebot`, a Discord bot front end exposing every
leaguectl command as a slash command (`/standings`, `/faab`, `/matchups`,
`/history`, `/rules`, `/managers`, `/announcements`, `/schedule`,
`/rivalries`, `/state`). Same core package, same caveats as the CLI
(`/announcements` and `/schedule` are placeholder data, `/rivalries` is
empty) — see "Running the Discord bot" below for setup.

**Phase 5 (done):** `Scoring()`, pulled live from Sleeper's
`league.scoring_settings` and grouped into categories (Passing, Rushing,
Receiving, Fumbles, Kicking, Defense/Special Teams, Points Allowed) with
human labels for Sleeper's stat-code vocabulary. Confirms the league is
half-PPR (`rec: 0.5`), resolving `../scoring.md`'s ambiguity. Exposed as
`leaguectl scoring` and `/scoring` (the latter as a Discord embed, since
the full output doesn't fit Discord's 2000-char message limit).

**Not built yet:**
- Rivalries sync job (the actual computation described above)
- Recap archive (revisiting later, per league discussion)
- Side pots (revisiting later, per league discussion)
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
./leaguectl scoring
./leaguectl managers
./leaguectl announcements
./leaguectl schedule
./leaguectl rivalries
./leaguectl state
```

All commands default `-league` to the Hit or Miss league ID from
`../readme.md`. Only `standings`, `faab`, `matchups`, `scoring` and `state`
call the live Sleeper API, so those need outbound network access to
`api.sleeper.app` (no auth/API key required); the rest run entirely off
embedded local data.

```sh
go test ./...
```

The Sleeper client and core package are tested against `httptest` fixtures
rather than the live API, so `go test` works offline.

## Running the Discord bot

One-time setup, on [Discord's Developer Portal](https://discord.com/developers/applications):

1. **New Application** → name it (e.g. "League Home") → **Bot** tab →
   **Reset Token** and copy it. This is `DISCORD_BOT_TOKEN` below; treat it
   like a password (whoever has it can act as the bot).
2. **OAuth2 → URL Generator** → scopes: `bot` and `applications.commands`.
   No privileged Gateway intents are needed (the bot only handles slash
   commands, not raw messages).
3. Open the generated URL, pick the league's Discord server, authorize it.
4. Right-click the server icon → **Copy Server ID** (enable Developer Mode
   under Discord's Advanced settings first if that option isn't visible).
   This is `DISCORD_GUILD_ID` below.

Then run it:

```sh
cd football/league_home/app
go build -o leaguebot ./cmd/leaguebot
DISCORD_BOT_TOKEN=<bot token> DISCORD_GUILD_ID=<server id> ./leaguebot
```

`DISCORD_GUILD_ID` scopes slash-command registration to that one server,
where Discord propagates new/changed commands within seconds; omit it to
register globally instead (every server the bot is in, but registration
can take up to an hour to show up). `LEAGUE_ID` overrides the default
league ID if needed, same as leaguectl's `-league` flag. The process stays
running handling slash commands until killed (Ctrl+C).
