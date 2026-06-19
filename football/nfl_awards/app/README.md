# NFL Awards Reference — App

A small Go server that serves the filterable awards browser described in
`../docs/ui-spec.md`, reading `../data/nfl_awards_data.json` directly so the
app always reflects the latest dataset without a rebuild.

## Run locally

```sh
cd football/nfl_awards/app
go run . -addr :8080
```

Then open `http://localhost:8080`.

## Run on your desktop, reachable over Tailscale

The server already binds `0.0.0.0` when given a port-only address (e.g.
`:8080`), so any device on your tailnet can reach it once you point it at
your desktop's Tailscale IP or MagicDNS name — no extra config needed:

```sh
cd football/nfl_awards/app
go build -o nflawards .
./nflawards -addr :8080
```

Then from any device on your tailnet, browse to:

```
http://<your-desktop-tailscale-name>:8080
```

Find your desktop's Tailscale name/IP with `tailscale status` on the
desktop, or check it in the Tailscale admin console.

### Optional: clean HTTPS URL via `tailscale serve`

If you'd rather not deal with `http://host:8080` from other devices, run:

```sh
tailscale serve --bg 8080
```

This exposes the app at an HTTPS URL on your tailnet
(`https://<desktop-name>.<your-tailnet>.ts.net`) without managing certs —
Tailscale handles TLS. Run `tailscale serve --https=443 off` later to stop
exposing it.

### Running it persistently

To keep it running across reboots/logouts, wrap the built binary in a
systemd user service (Linux) or a LaunchAgent (macOS) that runs:

```
/path/to/nflawards -addr :8080 -data /path/to/football/nfl_awards/data/nfl_awards_data.json
```

## Flags

| Flag | Default | Purpose |
|---|---|---|
| `-addr` | `:8080` | Listen address. `:PORT` binds all interfaces (needed for Tailscale access); `127.0.0.1:PORT` restricts to localhost only. |
| `-data` | `../data/nfl_awards_data.json` | Path to the dataset. Override if running the binary from elsewhere. |

## Notes

- No authentication is implemented — access control relies entirely on
  Tailscale's network-level ACLs (only devices on your tailnet can reach the
  port). Don't expose this port on the open internet.
- No external network calls or API keys are needed; this only serves the
  static dataset already in this repo.
