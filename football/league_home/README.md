# League Home

A planned home base for the `Hit or Miss` league (see `../readme.md`):
live standings/matchups/FAAB pulled from Sleeper, plus the league's award
and governance history that predates Sleeper and isn't in its API.

## Design

One core data layer, three thin front ends on top of it — none of them
own league-data logic, they just call the same operations and format the
result differently:

```
        Sleeper API ─┐
                      ├─▶  core (internal/core)  ◀── data/history.json
   data/history.json ─┘            │
                  ┌─────────────────┼─────────────────┐
             leaguectl          leaguebot          leagueweb
            (CLI, Phase 1)   (Discord, Phase 4)   (Web, Phase 6)
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
- `cmd/leagueweb` — web UI front end. A thin JSON API (one endpoint per
  core operation) plus a static, build-step-free single-page app
  (`cmd/leagueweb/static`) that fetches and renders it, both embedded into
  one binary the same way `data/*.json` is.
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

**Phase 6 (done):** `cmd/leagueweb`, a web UI front end: a tabbed
single-page app (standings, FAAB, matchups, scoring, rules, managers,
history, announcements, schedule, rivalries) backed by a `/api/*` JSON
endpoint per core operation. Same core package, same caveats as the CLI
and Discord bot (announcements/schedule are placeholder data, rivalries
is empty) — see "Running the web UI" below.

**Not built yet:**
- Rivalries sync job (the actual computation described above)
- Recap archive (revisiting later, per league discussion)
- Side pots (revisiting later, per league discussion)

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

## Running the web UI

```sh
cd football/league_home/app
go build -o leagueweb ./cmd/leagueweb
./leagueweb
```

Serves on `:8081` by default (override with `-addr`); `LEAGUE_ID`
overrides the default league ID, same as the CLI and bot. `-prefix` mounts
the app under a path instead of root (e.g. `/leagueweb`) — see "Run on
your desktop, reachable over Tailscale" below for why you'd want that. Open
`http://localhost:8081` and pick a tab — each one lazily fetches its data
from a matching `/api/*` endpoint (`/api/standings`, `/api/faab`,
`/api/matchups?week=N`, `/api/scoring`, `/api/rules`, `/api/managers`,
`/api/history`, `/api/announcements`, `/api/schedule`, `/api/rivalries`,
`/api/state`) and caches it for the rest of the page session (matchups
excepted, since the week selector changes the query).

## Run on your desktop, reachable over Tailscale

The server binds `0.0.0.0` when given a port-only address (e.g. `:8081`),
so any device on your tailnet can reach it once pointed at your desktop's
Tailscale IP or MagicDNS name:

```sh
./leagueweb -addr :8081
```

Then from any device on your tailnet:

```
http://<your-desktop-tailscale-name>:8081
```

Find your desktop's Tailscale name/IP with `tailscale status`, or check the
Tailscale admin console.

### Optional: clean HTTPS URL via `tailscale serve`

```sh
tailscale serve --bg 8081
```

Exposes the app at `https://<desktop-name>.<your-tailnet>.ts.net` with
Tailscale handling TLS. Run `tailscale serve --https=443 off` to stop.

This maps the app to the hostname's root path (`/`). If you're also
running `nflawards` (the NFL Awards Reference app, see
`../nfl_awards/app/README.md`) on the same desktop and want both reachable
under one HTTPS hostname instead of separate ports, give each app its own
path with `-prefix` and mount each at a distinct path instead of root:

```sh
./leagueweb -addr :8081 -prefix /leagueweb
tailscale serve --bg /leagueweb http://127.0.0.1:8081
# (and, for nflawards: tailscale serve --bg /nflawards http://127.0.0.1:8080)
```

`-prefix` makes the app mount all its routes (static assets and `/api/*`)
under that path instead of root, so it works correctly behind a
`tailscale serve` path mount (which forwards the full request path,
including the prefix, to the backend — it does not strip it). Now both
apps are reachable at:

```
https://<desktop-name>.<your-tailnet>.ts.net/leagueweb
https://<desktop-name>.<your-tailnet>.ts.net/nflawards
```

Check current mappings with `tailscale serve status`; remove one with
`tailscale serve --bg off /leagueweb` (same mount point).

### Running it persistently (macOS, via launchd)

`com.leagueweb.serve.plist.template` is checked in alongside the app. Copy
it into place, fill in the two `/REPLACE/WITH/...` placeholders with real
absolute paths, then load it:

```sh
cp com.leagueweb.serve.plist.template ~/Library/LaunchAgents/com.leagueweb.serve.plist
# edit ~/Library/LaunchAgents/com.leagueweb.serve.plist: fill in both
# /REPLACE/WITH/ABSOLUTE/PATH/TO/... placeholders (binary path + WorkingDirectory)
launchctl load ~/Library/LaunchAgents/com.leagueweb.serve.plist
```

If you're using the shared-hostname `-prefix` setup above, add `-prefix`
`/leagueweb` to the `ProgramArguments` array in the plist before loading
it.

`RunAtLoad` + `KeepAlive` mean `leagueweb` starts on login and restarts if
it crashes. The `tailscale serve` mapping from the previous section
persists on its own across Tailscale restarts/reboots, so that's a
one-time setup, not a per-boot task. Logs land in `/tmp/leagueweb.log` and
`/tmp/leagueweb.error.log`. To override the league, uncomment the
`EnvironmentVariables` block in the plist before loading it. To stop it:

```sh
launchctl unload ~/Library/LaunchAgents/com.leagueweb.serve.plist
```

On Linux, use an equivalent `systemd --user` unit instead (same idea,
different syntax) running:

```
/path/to/leagueweb -addr :8081
```

### Notes

- No authentication is implemented — access control relies entirely on
  Tailscale's network-level ACLs (only devices on your tailnet can reach the
  port). Don't expose this port on the open internet.
- `leagueweb` has no database; it calls the live Sleeper API plus the
  repo's embedded JSON data on every request, so no `-db` flag or one-time
  setup step is needed beyond building the binary.
