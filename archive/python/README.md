# Archived: Python

`get_touchdowns.py` — a standalone script (using `sleeper_wrapper`) that
pulls a week's player stats from Sleeper to surface touchdowns. Superseded
by `league_home/app`'s Go-based `internal/sleeper` client, which already
talks to the same API for standings/matchups/scoring.

Not reimplemented yet — tracked as a future `leaguectl`/`leagueweb` feature
(weekly touchdown leaders/highlights) in `league_home/README.md`'s "Not
built yet" list. `requirements.txt` and `.python-version` are kept
alongside it for when that happens.
