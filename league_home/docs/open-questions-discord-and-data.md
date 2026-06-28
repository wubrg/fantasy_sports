# Open Questions: Discord Integration + More Hit or Miss Data

This is a list of decisions for you to make, not a design doc. Each
question below is either already answered by how the code works today
(marked **Resolved**) or still needs your input (marked **Open**).

## Discord integration

### Resolved: Tailscale-only hosting does not block the Discord bot

`cmd/leaguebot` (in this repo, already built) connects to Discord over
its **Gateway API** — an outbound websocket connection the bot process
initiates to Discord's servers, the same way a phone or desktop Discord
client connects. It does not listen on any inbound port, doesn't need a
public DNS name or TLS cert, and isn't reachable "from the internet" in
the way `leagueweb`/`canton` are. Staying behind Tailscale and never
exposing those two web UIs publicly has no bearing on whether the bot
works — they're unrelated network paths. The bot already runs and
answers slash commands today if you start it with a bot token (see
"Running the Discord bot" in the main README).

So: there's no missing piece for "how does the bot work without a real
website" — it never needed one. What's still open is operational, below.

### Open: where/how does the bot run persistently?

`leagueweb` and `canton` both have a launchd plist template + Makefile
targets (`leagueweb-load`, `canton-load`, etc.) for "start on login,
restart on crash." `leaguebot` has none of that yet — right now it's a
foreground process you start manually and it dies if your terminal
closes or your Mac sleeps/reboots.

- Do you want the same launchd-plist treatment for `leaguebot` (a
  `com.leaguebot.serve.plist.template` + Makefile targets), so it runs
  continuously on whichever Mac is left on?
- Or is occasional/manual use fine (start it before league chat is
  active, kill it after)?

### Open: where does the bot token live?

Discord bot tokens are secrets (whoever has one can act as the bot in
every server it's joined). Right now the README has you pass it as an
env var on the command line. If the bot runs persistently via launchd,
the token needs to live somewhere the plist can read it (the
`EnvironmentVariables` block, a `.env` file the binary loads, etc.) —
who manages that file/keychain entry, and is it git-ignored everywhere
it might land?

### Open: which Discord server, and who manages the bot's Discord App?

The bot needs to be invited into one specific Discord server (the
league's), and someone needs to own the underlying Discord Developer
Portal application (the one holding the bot token, can regenerate it,
sees its OAuth scopes). Is that you, or someone else in the league?

### Open: should slash-command replies link to `leagueweb`?

Some commands' answers might naturally want to link out for more detail
(e.g. `/matchups` replying "full box score: <url>"). Since `leagueweb`
is Tailscale-only, that link would 404/timeout for any league member not
on your tailnet. Options:
- Never link out; keep every reply fully self-contained text/embed (the
  current behavior — no command does this today, so no change needed
  unless you want richer replies later).
- Link out anyway, accepting that only tailnet members can follow it.
- Get `leagueweb` reachable by the whole league some other way (a real
  public deployment, `tailscale serve` + sharing your tailnet with
  league members via Tailscale's invite mechanism, etc.) — a bigger
  decision than this doc is scoped to make for you.

## More Hit or Miss league data in the app

The core package (`internal/core`) already exposes `Standings`, `Faab`,
`Matchups`, `History`, `Rules`, `Scoring`, `Managers`, `Announcements`,
`Schedule`, `Rivalries`, `State` — all wired into `leaguectl`,
`leaguebot`, and `leagueweb` identically. Three of those ship with
placeholder or empty data today (see main README's Status section):

- **`Announcements`** — example entries only. Real data needs some way
  to *write* entries (a Discord-message-reading bot, or a small posting
  CLI/web form). **Open: do you want this, and if so, sourced from
  Discord messages, a manual entry tool, or something else?**
- **`Schedule`** — illustrative entries derived from `Rules`, not real
  calendar dates. Real dates live in the league's Google Calendar
  (`leagues/hit_or_miss/communication.md`). **Open: is a one-time or
  periodic sync from that calendar into `data/schedule.json` worth
  building, or is manual transcription often enough (it rarely
  changes)?**
- **`Rivalries`** — ships empty. Needs walking each season's
  `previous_league_id` chain through Sleeper and aggregating every
  historical matchup into head-to-head records — real computation, not
  yet built. **Open: worth prioritizing, or low value to you?**

Beyond those three, the main README's "Not built yet" list has:
- Recap archive — `leagues/hit_or_miss/recaps/2021/*.md` already exist
  as hand-written markdown; surfacing them through `leaguectl
  recaps`/`leagueweb` would just be reading files that exist, no new
  data source needed. **Open: want this added as a real feature?**
- Side pots — previously deferred "per league discussion." **Open: any
  update, or still deferred?**
- Weekly touchdown leaders/highlights — the archived
  `archive/python/get_touchdowns.py` script did this against Sleeper
  directly; redoing it on top of the existing `internal/sleeper` client
  would be new, not yet scoped. **Open: worth building, and if so, does
  "touchdowns" mean league-wide stat leaders, or just your own roster's
  scoring breakdown?**

Anything else you want to see that isn't on this list — say so and it
can be scoped as a new `core` operation the same way the others were.
