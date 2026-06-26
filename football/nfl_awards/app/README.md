# NFL Awards Reference — App

A small Go server that serves the filterable awards browser described in
`../docs/ui-spec.md`, backed by a SQLite database (`../data/nfl_awards.db`).
A companion CLI, `nflctl`, is how you add, remove, and look up rows.

The database enforces data quality directly: team/unit/award codes are
foreign keys into fixed reference tables (so a typo'd team code is rejected,
not silently stored), and a unique constraint blocks exact duplicate rows.

`../data/nfl_awards_data.json` is kept as a version-controlled, diffable
snapshot — not the live data source. Regenerate it after making changes with
`nflctl export-json` (see below) so `git diff` shows what changed.

## One-time setup

The SQLite file isn't checked into git (binary, no useful diffs). Build it
from the JSON snapshot:

```sh
cd football/nfl_awards/app
go build -o nflctl ./cmd/nflctl
./nflctl import ../data/nfl_awards_data.json
```

This creates `../data/nfl_awards.db`, applies the schema, seeds the
team/unit/award reference tables, and loads all 4,498 rows. Safe to re-run —
already-imported rows are skipped as duplicates, not errors.

## Updating data

```sh
# Add a row (year/player/pos/unit/team/award are required; notes optional)
./nflctl add -year 2026 -player "Some Player" -pos WR -unit O -team KC -award "Pro Bowl"

# Remove a row by id (find the id with `list` first)
./nflctl rm -id 4321

# Look something up
./nflctl list -team KC -year 2025
./nflctl list -player Mahomes

# Refresh the git-tracked JSON snapshot after a batch of changes
./nflctl export-json ../data/nfl_awards_data.json
```

`add` rejects invalid team/unit/award codes (foreign key violation) and
exact duplicates (unique constraint violation) with a clear error instead of
silently corrupting the dataset.

Valid `-unit` values: `O`, `D`, `ST`. Valid `-team`/`-award` codes are listed
in `../docs/ui-spec.md` (or run `./nflctl list` and look at existing rows).

## Run the web app

```sh
cd football/nfl_awards/app
go build -o nflawards .
./nflawards -addr :8080
```

Then open `http://localhost:8080`. The app reads live from the SQLite file
on every request, so changes made with `nflctl` show up on refresh — no
restart needed.

## Run on your desktop, reachable over Tailscale

The server binds `0.0.0.0` when given a port-only address (e.g. `:8080`),
so any device on your tailnet can reach it once pointed at your desktop's
Tailscale IP or MagicDNS name:

```sh
./nflawards -addr :8080
```

Then from any device on your tailnet:

```
http://<your-desktop-tailscale-name>:8080
```

Find your desktop's Tailscale name/IP with `tailscale status`, or check the
Tailscale admin console.

### Optional: clean HTTPS URL via `tailscale serve`

```sh
tailscale serve --bg 8080
```

Exposes the app at `https://<desktop-name>.<your-tailnet>.ts.net` with
Tailscale handling TLS. Run `tailscale serve --https=443 off` to stop.

This maps the app to the hostname's root path (`/`). If you're also running
`leagueweb` (the League Home web UI) on the same desktop and want both
reachable under one HTTPS hostname instead of separate ports, give each app
its own path:

```sh
tailscale serve --bg --set-path=/nflawards localhost:8080
tailscale serve --bg --set-path=/leagueweb localhost:8081
```

`tailscale serve --set-path` strips the mount path before forwarding to
the backend (a request to `https://<host>.ts.net/nflawards/foo` arrives at
the backend as plain `GET /foo`), so `nflawards` needs no path-prefix
awareness of its own — it just serves everything at root, same as always.
Both apps are reachable at:

```
https://<desktop-name>.<your-tailnet>.ts.net/nflawards
https://<desktop-name>.<your-tailnet>.ts.net/leagueweb
```

Check current mappings with `tailscale serve status`; remove one with
`tailscale serve --set-path=/nflawards off`.

### Running it persistently (macOS, via launchd)

`com.nflawards.serve.plist.template` is checked in alongside the app. Copy
it into place, fill in the three `/REPLACE/WITH/...` placeholders with real
absolute paths (binary, db, and `WorkingDirectory`), then load it:

```sh
cp com.nflawards.serve.plist.template ~/Library/LaunchAgents/com.nflawards.serve.plist
# edit ~/Library/LaunchAgents/com.nflawards.serve.plist: fill in all three
# /REPLACE/WITH/ABSOLUTE/PATH/TO/... placeholders
launchctl load ~/Library/LaunchAgents/com.nflawards.serve.plist
```

`RunAtLoad` + `KeepAlive` mean `nflawards` starts on login and restarts if
it crashes. The `tailscale serve` mapping persists on its own across
Tailscale restarts/reboots, so that's a one-time setup, not a per-boot
task. Logs land in `/tmp/nflawards.log` and `/tmp/nflawards.error.log`. To
stop it:

```sh
launchctl unload ~/Library/LaunchAgents/com.nflawards.serve.plist
```

On Linux, use an equivalent `systemd --user` unit instead (same idea,
different syntax).

## Flags

**`nflawards`** (web server)

| Flag | Default | Purpose |
|---|---|---|
| `-addr` | `:8080` | Listen address. `:PORT` binds all interfaces (needed for Tailscale); `127.0.0.1:PORT` restricts to localhost. |
| `-db` | `../data/nfl_awards.db` | Path to the SQLite database. |

**`nflctl`** (admin CLI) — every subcommand accepts `-db PATH` (same default).

| Command | Purpose |
|---|---|
| `import JSON_FILE` | Bulk-load rows from a JSON snapshot (idempotent) |
| `add -year -player -pos -unit -team -award [-notes]` | Add one validated row |
| `rm -id ID` | Remove a row |
| `list [-year] [-team] [-award] [-player]` | Filtered lookup |
| `export-json OUT_FILE` | Write the current dataset back to JSON |

## Notes

- No authentication is implemented — access control relies entirely on
  Tailscale's network-level ACLs (only devices on your tailnet can reach the
  port). Don't expose this port on the open internet.
- No external network calls or API keys are needed.
- SQLite is single-writer: running `nflctl` while `nflawards` is serving is
  fine for the occasional add/remove this dataset sees (WAL mode is enabled
  so reads aren't blocked by a write), but don't script concurrent bulk
  writes from multiple processes.
