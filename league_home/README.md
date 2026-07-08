# League Home

A planned home base for the `Hit or Miss` league (see `../leagues/hit_or_miss/readme.md`):
live standings/matchups/FAAB pulled from Sleeper, the league's award and
governance history that predates Sleeper and isn't in its API, and (as a
scaffold pending real auth credentials) the league's pre-Sleeper seasons
from ESPN.

## Design

One core data layer, three thin front ends on top of it — none of them
own league-data logic, they just call the same operations and format the
result differently:

```
        Sleeper API ─┐
           ESPN API ─┼─▶  core (internal/core)  ◀── data/history.json
   data/history.json ─┘            │
                  ┌─────────────────┼─────────────────┐
             leaguectl          leaguebot          leagueweb
            (CLI, Phase 1)   (Discord, Phase 4)   (Web, Phase 6)
```

(`leaguebot`/`leagueweb` only call the Sleeper-era operations today; the
ESPN-era `Historical*` operations are wired into `leaguectl` only so far —
see Phase 8 below.)

- `internal/sleeper` — minimal client for the public, keyless Sleeper API
  (league settings, rosters, users, matchups, NFL state).
- `internal/espn` — client for ESPN's fantasy football API, covering the
  league's history from before it migrated to Sleeper. Unlike Sleeper,
  ESPN has no keyless public read access for league history; a private
  league needs the `espn_s2`/`SWID` auth cookies from a member's browser
  session (see "Running ESPN history commands" below).
- `internal/core` — normalizes Sleeper/ESPN data + the local JSON into the
  operations every front end will call: `Standings`, `Faab`, `Matchups`,
  `History`, `Rules`, `Scoring`, `Managers`, `Announcements`, `Schedule`,
  `Rivalries`, `State`, `Seasons`, and the ESPN-era `Historical*` methods
  (`HistoricalSeasons`, `HistoricalStandings`, `HistoricalMatchups`,
  `HistoricalDraft`), available on a `Service` only after calling
  `WithESPN`/`WithESPNClient`.
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
  (2014-2022), transcribed from `../leagues/hit_or_miss/league_fees_and_dues.md` and
  `../leagues/hit_or_miss/league_members.md`. Embedded into the binary at build time.
- `data/rules.json` — current-season ruleset (roster slots, keepers,
  waivers, draft, trade deadline, playoffs, governance), transcribed from
  `../leagues/hit_or_miss/rosters.md`, `../leagues/hit_or_miss/draft.md`, `../leagues/hit_or_miss/league_fees_and_dues.md` and
  `../leagues/hit_or_miss/policies_and_procedures.md`. Holds today's rules only, not a
  history of past changes. Embedded into the binary at build time.
- `data/managers.json` — every manager who's ever owned a team, past and
  present, with name-spelling aliases (e.g. "Chris Bushjost" /
  "Chris Buschjost") so history and Sleeper data can be joined to one
  stable identity regardless of which spelling/season used. Transcribed
  from `../leagues/hit_or_miss/league_members.md` plus the variants in `data/history.json`.
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
risked silently drifting out of sync (as `../leagues/hit_or_miss/scoring.md`'s own half-PPR
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
  `../leagues/hit_or_miss/communication.md`.
- `Rivalries` — ships an intentionally empty dataset. Real head-to-head
  records need walking each season's `previous_league_id` chain back
  through Sleeper and aggregating every matchup, which needs live Sleeper
  API access this sandboxed environment doesn't have. Fabricating
  win/loss numbers for real people instead would just be wrong, so the
  schema exists and the data waits for a real sync job.

**Phase 4 (done):** `cmd/leaguebot`, a Discord bot front end exposing every
leaguectl command as a slash command (`/standings`, `/faab`, `/matchups`,
`/history`, `/rules`, `/managers`, `/announcements`, `/schedule`,
`/rivalries`, `/state`, `/seasons`). Same core package, same caveats as the CLI
(`/announcements` and `/schedule` are placeholder data, `/rivalries` is
empty) — see "Running the Discord bot" below for setup.

**Phase 5 (done):** `Scoring()`, pulled live from Sleeper's
`league.scoring_settings` and grouped into categories (Passing, Rushing,
Receiving, Fumbles, Kicking, Defense/Special Teams, Points Allowed) with
human labels for Sleeper's stat-code vocabulary. Confirms the league is
half-PPR (`rec: 0.5`), resolving `../leagues/hit_or_miss/scoring.md`'s ambiguity. Exposed as
`leaguectl scoring` and `/scoring` (the latter as a Discord embed, since
the full output doesn't fit Discord's 2000-char message limit).

**Phase 6 (done):** `cmd/leagueweb`, a web UI front end: a tabbed
single-page app (standings, FAAB, matchups, scoring, rules, managers,
history, announcements, schedule, rivalries) backed by a `/api/*` JSON
endpoint per core operation, plus a season selector in the header (see
Phase 7 below). Same core package, same caveats as the CLI
and Discord bot (announcements/schedule are placeholder data, rivalries
is empty) — see "Running the web UI" below.

**Phase 7 (done):** `Seasons()`, which walks a league's
`previous_league_id` chain back through Sleeper to list every season it's
had its own league ID for, most recent first (Sleeper terminates the
chain either with an empty/`"0"` `previous_league_id` or by omitting the
field, depending on league age — both are handled). Exposed as
`leaguectl seasons`, `/seasons`, and `/api/seasons`. `leagueweb`'s season
selector uses this to let the tabbed UI show a past season's standings,
FAAB, matchups or scoring instead of only the current one — see "Running
the web UI" below for how the `?league=` override works.

**Phase 8 (scaffold only, no live data yet):** `internal/espn`, a client
for the Hit or Miss league's pre-Sleeper seasons on ESPN (league ID
`56226`, from before the 2023 migration), plus the matching `Historical*`
core operations (`HistoricalSeasons`, `HistoricalStandings`,
`HistoricalMatchups`, `HistoricalDraft`) and four `leaguectl` commands
(`espn-seasons`, `espn-standings`, `espn-matchups`, `espn-draft`) — see
"Running ESPN history commands" below. Unlike Sleeper, ESPN has no
keyless public read access to league history; this league is private, so
every one of these calls needs the `espn_s2`/`SWID` auth cookies from a
league member's logged-in browser session, which this environment doesn't
have. The client, core operations, and CLI commands are built, tested
against `httptest` fixtures, and confirmed to fail with a clear,
actionable error (rather than a confusing JSON-parse failure) when run
without credentials — but none of it has been exercised against real ESPN
data yet, and it isn't wired into `leaguebot` or `leagueweb` yet either.
That's left for once real `ESPN_S2`/`ESPN_SWID` values are available to
validate against.

**Not built yet:**
- ESPN historical data, validated against the real league (Phase 8 above
  is scaffold/plumbing only, pending real `espn_s2`/`SWID` credentials)
- ESPN history wired into `leaguebot`/`leagueweb` (CLI-only for now)
- ESPN draft pick player names — `HistoricalDraft` returns ESPN's raw
  numeric player ID only; resolving it to a name needs a separate lookup
  against ESPN's player database, not yet implemented
- Rivalries sync job (the actual computation described above)
- Recap archive (revisiting later, per league discussion)
- Side pots (revisiting later, per league discussion)
- Weekly touchdown leaders/highlights — `../archive/python/get_touchdowns.py`
  did this against the Sleeper API directly; a `leaguectl`/`leagueweb`
  equivalent built on the existing `internal/sleeper` client would replace
  it, but hasn't been built yet.

## Running it

```sh
cd league_home/app
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
./leaguectl seasons
```

All commands default `-league` to the Hit or Miss league ID from
`../leagues/hit_or_miss/readme.md`. Only `standings`, `faab`, `matchups`, `scoring`, `state`
and `seasons` call the live Sleeper API, so those need outbound network access to
`api.sleeper.app` (no auth/API key required); the rest run entirely off
embedded local data. Pass a season's league ID from `seasons`' output to
any other command's `-league` flag (except `state`, which is global NFL
state, not league-specific) to query that season instead of the current
one.

```sh
go test ./...
```

The Sleeper client and core package are tested against `httptest` fixtures
rather than the live API, so `go test` works offline.

## Running ESPN history commands

```sh
cd league_home/app
go build -o leaguectl ./cmd/leaguectl
ESPN_S2=<espn_s2 cookie value> ESPN_SWID=<SWID cookie value> ./leaguectl espn-seasons
ESPN_S2=<espn_s2 cookie value> ESPN_SWID=<SWID cookie value> ./leaguectl espn-standings -year 2020
ESPN_S2=<espn_s2 cookie value> ESPN_SWID=<SWID cookie value> ./leaguectl espn-matchups -year 2020 -week 1
ESPN_S2=<espn_s2 cookie value> ESPN_SWID=<SWID cookie value> ./leaguectl espn-draft -year 2020
```

These cover the league's pre-Sleeper seasons (ESPN league ID `56226`,
overridable with `-espn-league`). Unlike the Sleeper-backed commands
above, ESPN has no keyless public read access to league history, and this
league is private, so every `espn-*` command needs `ESPN_S2`/`SWID` — the
`espn_s2`/`SWID` cookie values from a league member's session, logged in
at fantasy.espn.com (open browser dev tools → Application/Storage →
Cookies → `fantasy.espn.com`, copy both values). They're read from the
environment only, never as a `-flag`, so they don't end up in shell
history or a `ps aux` listing. Without them, every `espn-*` command fails
fast with a clear error instead of a confusing one (e.g. `espn: GET
/leagueHistory/56226: redirected to https://www.espn.com/fantasy/ (this
league is private and needs valid espn_s2/SWID, or they've expired)`).

`espn-seasons` lists every year ESPN has data for under this league ID;
pass one of those years to `-year` on the other three commands. None of
this is wired into `leaguebot` or `leagueweb` yet (see Phase 8 above), and
none of it has been run against real ESPN data in this environment, since
no `espn_s2`/`SWID` values are available here.

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
cd league_home/app
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
cd league_home/app
go build -o leagueweb ./cmd/leagueweb
./leagueweb
```

Serves on `:8081` by default (override with `-addr`); `LEAGUE_ID`
overrides the default league ID, same as the CLI and bot. Open
`http://localhost:8081` and pick a tab — each one lazily fetches its data
from a matching `/api/*` endpoint (`/api/standings`, `/api/faab`,
`/api/matchups?week=N`, `/api/scoring`, `/api/rules`, `/api/managers`,
`/api/history`, `/api/announcements`, `/api/schedule`, `/api/rivalries`,
`/api/state`, `/api/seasons`) and caches it for the rest of the page session
(matchups excepted, since the week selector changes the query).

The header's season selector (populated from `/api/seasons`) lets you
view a past season instead of the current one. Switching it appends
`?league=<season's league ID>` to the four Sleeper-backed, season-scoped
endpoints — `/api/standings`, `/api/faab`, `/api/matchups`, `/api/scoring`
— which then query that season's data instead of the server's configured
default. The other tabs (rules, managers, history, announcements,
schedule, rivalries) are locally-curated and not season-scoped, and
`/api/state` reports the live, current NFL week regardless of league, so
none of them are affected by the selector.

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
running `canton` (the NFL Awards Reference app, see
`../canton/app/README.md`) on the same desktop and want both reachable
under one HTTPS hostname instead of separate ports, give each app its own
path:

```sh
tailscale serve --bg --set-path=/leagueweb localhost:8081
tailscale serve --bg --set-path=/canton localhost:8080
```

`tailscale serve --set-path` strips the mount path before forwarding to
the backend (a request to `https://<host>.ts.net/leagueweb/foo` arrives at
the backend as plain `GET /foo`), so `leagueweb` needs no path-prefix
awareness of its own — it just serves everything at root, same as always.
Both apps are reachable at:

```
https://<desktop-name>.<your-tailnet>.ts.net/leagueweb
https://<desktop-name>.<your-tailnet>.ts.net/canton
```

Check current mappings with `tailscale serve status`; remove one with
`tailscale serve --set-path=/leagueweb off`.

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
