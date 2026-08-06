#!/usr/bin/env python3
"""Extract the Big 3 running back traits from a Fantasy Points article save.

Kyle Menton's "Anatomy of a League-Winning Running Back" identifies three
traits that separate league-winning backs, and states each candidate's prior
season numbers against them. Those numbers are structured data living in
prose, so they are pulled out here rather than read by eye.

The article's thresholds, over the last five seasons, for a top-6 finish:

    6.0+ receiving FPG          (most important, and most talent-driven)
    3.5+ explosive rushing FPG  (RB skill plus offensive line)
    3.5+ inside-10 rushing FPG  (mostly team-driven)

100% of top-6 RBs cleared at least one; 56.7% cleared exactly one.

Two cautions carried into the output rather than left implicit:

1. These are FULL-PPR figures. Hit or Miss is half-PPR, so the receiving
   column overstates its own importance here — the article's own basis is
   targets being worth 2.55x a carry, which is a full-PPR number. A
   half-PPR receiving figure is emitted alongside the published one.
2. The charts in the article are not recoverable from a page save; they are
   lazy-loaded and absent even from a full "save page as" capture. Only the
   inline numbers are extractable.

Usage:
    python3 extract_fantasypoints.py <article.html> <out.csv>

Dependency-free: stdlib only.
"""

import csv
import html
import re
import sys

# Each candidate reads:
#   "Chase Brown, RB, CIN  Redraft ESPN ADP: 26.2 (RB13)
#    2025 Metrics: 8.4 Rec FPG (RB5), 1.3 Explosive FPG (RB34), 2.0 Inside-10 FPG (RB24)"
#
# Matched against stripped text rather than markup: the page repeats each
# name in a table of contents, and requiring the ADP and metrics to follow
# immediately is what separates a real section from its TOC entry.
#
# Whitespace is permitted before each comma because the name sits in its own
# heading element, so stripping tags yields "Chase Brown , RB, CIN". The name
# is anchored to a line start so it cannot absorb the tail of the preceding
# paragraph.
CANDIDATE_RE = re.compile(
    r"^([A-Z][A-Za-z.'’\-]+(?:[ ][A-Z][A-Za-z.'’\-]+)*)\s*,\s*(RB|WR|TE|QB)\s*,\s*([A-Z]{2,3})\s+"
    r"(?:Redraft\s+)?ESPN\s+(?:Redraft\s+)?ADP:\s*([\d.]+)\s*\(([A-Z]{2}\d+)\)\s*"
    r"\d{4}\s*Metrics:\s*"
    r"([\d.]+)\s*Rec\s*FPG\s*\(([A-Z]{2}\d+)\),\s*"
    r"([\d.]+)\s*Explosive\s*FPG\s*\(([A-Z]{2}\d+)\),\s*"
    r"([\d.]+)\s*Inside-?10\s*FPG\s*\(([A-Z]{2}\d+)\)",
    re.I | re.M,
)

# The article's own thresholds for a top-6 finish.
THRESHOLD_REC = 6.0
THRESHOLD_EXPLOSIVE = 3.5
THRESHOLD_INSIDE10 = 3.5

# Receptions per game is not published per player, so the half-PPR
# adjustment uses the league-average receiving yards per reception for backs
# to back out an approximate reception count. Crude on purpose, and flagged
# as an estimate in the output rather than presented as measured.
RB_YARDS_PER_RECEPTION = 7.5
FULL_PPR_POINTS_PER_RECEPTION = 1.0
HALF_PPR_POINTS_PER_RECEPTION = 0.5


def strip_tags(markup):
    """Flatten markup to text, preserving element boundaries as newlines.

    Collapsing tags to spaces would let the candidate pattern run backwards
    across a paragraph break and swallow the end of the previous sentence
    into the player's name ("price tag. Ashton Jeanty"). Newlines keep each
    heading on its own line so the name can be anchored to a line start.
    """
    text = html.unescape(re.sub(r"<[^>]+>", "\n", markup))
    text = re.sub(r"[ \t]+", " ", text)
    text = "\n".join(line.strip() for line in text.split("\n"))
    return re.sub(r"\n{2,}", "\n", text)


def half_ppr_receiving_fpg(full_ppr_fpg):
    """Restate a full-PPR receiving FPG in half-PPR terms.

    Receiving FPG is yards/10 + receptions x points-per-reception + TD
    contribution. Without a published reception count the split cannot be
    exact, so receptions are approximated from the yardage implied by the
    remainder. The result is an estimate and the caller labels it as one.
    """
    # Solve for receptions assuming yardage and receptions dominate the FPG.
    # fpg ~= (rec * ypr)/10 + rec * 1.0  ->  rec ~= fpg / (ypr/10 + 1)
    receptions = full_ppr_fpg / (RB_YARDS_PER_RECEPTION / 10 + FULL_PPR_POINTS_PER_RECEPTION)
    delta = receptions * (FULL_PPR_POINTS_PER_RECEPTION - HALF_PPR_POINTS_PER_RECEPTION)
    return round(full_ppr_fpg - delta, 2)


def extract(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        markup = f.read()
    markup = re.sub(r"<script.*?</script>", "", markup, flags=re.S)

    text = strip_tags(markup)

    rows = []
    seen = set()
    for m in CANDIDATE_RE.finditer(text):
        name, position, team = m.group(1).strip(), m.group(2).upper(), m.group(3).upper()
        if name in seen:
            continue
        seen.add(name)
        adp, adp_rank = m.group(4), m.group(5)
        rec, rec_rank = float(m.group(6)), m.group(7)
        exp, exp_rank = float(m.group(8)), m.group(9)
        in10, in10_rank = float(m.group(10)), m.group(11)

        traits = sum([
            rec >= THRESHOLD_REC,
            exp >= THRESHOLD_EXPLOSIVE,
            in10 >= THRESHOLD_INSIDE10,
        ])
        rows.append({
            "source": "fantasypoints-menton",
            "player": name,
            "position": position,
            "team": team,
            "espn_adp": adp,
            "adp_pos_rank": adp_rank,
            "rec_fpg": rec,
            "rec_pos_rank": rec_rank,
            "rec_fpg_half_ppr_est": half_ppr_receiving_fpg(rec),
            "explosive_fpg": exp,
            "explosive_pos_rank": exp_rank,
            "inside10_fpg": in10,
            "inside10_pos_rank": in10_rank,
            "traits_hit": traits,
            "hits_receiving": int(rec >= THRESHOLD_REC),
            "hits_explosive": int(exp >= THRESHOLD_EXPLOSIVE),
            "hits_goal_line": int(in10 >= THRESHOLD_INSIDE10),
        })
    return rows


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    rows = extract(sys.argv[1])
    if not rows:
        sys.exit("extract_fantasypoints: no candidate sections found — is this the article save?")

    with open(sys.argv[2], "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)

    print(f"wrote {len(rows)} candidates to {sys.argv[2]}")
    for r in sorted(rows, key=lambda r: -r["traits_hit"]):
        marks = "".join(
            c for c, hit in zip("REG", (r["hits_receiving"], r["hits_explosive"], r["hits_goal_line"])) if hit
        )
        print(f"  {r['player']:<20} {r['traits_hit']} trait(s) [{marks:<3}] "
              f"rec {r['rec_fpg']:>4} (half-PPR est {r['rec_fpg_half_ppr_est']:>4})  "
              f"exp {r['explosive_fpg']:>4}  in10 {r['inside10_fpg']:>4}  ADP {r['espn_adp']}")


if __name__ == "__main__":
    main()
