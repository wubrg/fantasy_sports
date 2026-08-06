#!/usr/bin/env python3
"""Tests for the source extractors. Run: python3 tools/test_extractors.py

These guard one specific trap. Subvertadown's value cell contains both a
<template> tooltip body and the ECR arrow icons. Strip the markup naively and
the tooltip's prose lands in the dollar value; strip the whole cell and the
arrows — real signal — are lost. Both must be handled, in the right order.
"""

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import extract_fantasypoints as fp  # noqa: E402
import extract_subvertadown as sv  # noqa: E402


def sheet(rows):
    """Wrap row markup in the minimal page structure the extractor expects."""
    header = (
        "<tr><th></th><th>Position</th><th></th></tr>"
        "<tr><th></th><th>Player</th><th>Q</th><th>R</th><th>W</th><th>T</th>"
        "<th>Tm</th><th>B</th><th>AAV</th><th>PS</th><th>$</th></tr>"
    )
    return f"<html><body><table>{header}{''.join(rows)}</table></body></html>"


TOOLTIP = (
    '<template x-ref="tooltipTemplate"><span class="font-sans">Indicates whether any '
    "expert contributor to FantasyPros’ ECR thinks the given player is likely to "
    "score significantly more or less than the player’s consensus ranking."
    "</span></template>"
)


def row(pos_rank, name, team, bye, aav, ps, value, up=False, down=False):
    icons = ""
    if up:
        icons += '<i class="ri-arrow-up-line text-blue-500"></i>'
    if down:
        icons += '<i class="ri-arrow-down-line text-red-500"></i>'
    tip = f'<span class="normal-case">{TOOLTIP}{icons}</span>' if icons else ""
    return (
        f"<tr><td>{pos_rank}</td><td>{name} {team}-1</td><td></td><td>R1</td>"
        f"<td></td><td></td><td>{team}-1</td><td>{bye}</td>"
        f"<td>${aav}</td><td>{ps}%</td><td>{tip}<span>${value}</span></td></tr>"
    )


class ExtractSubvertadownTest(unittest.TestCase):
    def parse(self, rows):
        with tempfile.NamedTemporaryFile("w", suffix=".html", delete=False) as f:
            f.write(sheet(rows))
            path = f.name
        try:
            return sv.parse_sheet(path, "beerplus")
        finally:
            os.unlink(path)

    def test_plain_row(self):
        got = self.parse([row("RB1", "Jahmyr Gibbs", "DET", 8, 68, 93, 73)])
        self.assertEqual(len(got), 1)
        r = got[0]
        self.assertEqual(r["player"], "Jahmyr Gibbs")
        self.assertEqual(r["position"], "RB")
        self.assertEqual(r["pos_rank"], 1)
        self.assertEqual(r["team"], "DET")
        self.assertEqual(r["bye"], 8)
        self.assertEqual((r["aav"], r["ps_pct"], r["value"]), (68, 93, 73))
        self.assertEqual((r["ecr_up"], r["ecr_down"]), (0, 0))
        self.assertEqual(r["baseline"], "beerplus")

    def test_tooltip_does_not_corrupt_the_value(self):
        """The tooltip mentions no digits, but its prose must not reach the
        value column — and the arrow beside it must still be read."""
        got = self.parse([row("WR3", "Jaxon Smith-Njigba", "SEA", 11, 58, 81, 63, down=True)])
        r = got[0]
        self.assertEqual(r["value"], 63)
        self.assertEqual(r["aav"], 58)
        self.assertEqual((r["ecr_up"], r["ecr_down"]), (0, 1))

    def test_up_arrow(self):
        got = self.parse([row("RB4", "Jonathan Taylor", "IND", 13, 54, 70, 55, up=True)])
        self.assertEqual((got[0]["ecr_up"], got[0]["ecr_down"]), (1, 0))

    def test_both_arrows_mean_contested(self):
        """Justin Jefferson really does carry both in the live sheet: some
        experts see upside, others downside. They must not cancel out."""
        got = self.parse([row("WR6", "Justin Jefferson", "MIN", 6, 47, 67, 44, up=True, down=True)])
        r = got[0]
        self.assertEqual((r["ecr_up"], r["ecr_down"]), (1, 1))
        self.assertEqual(r["value"], 44)

    def test_header_and_short_rows_are_skipped(self):
        got = self.parse(["<tr><td>junk</td></tr>", row("TE1", "Brock Bowers", "LV", 13, 34, 80, 27)])
        self.assertEqual(len(got), 1)
        self.assertEqual(got[0]["player"], "Brock Bowers")

    def test_team_suffix_stripped_from_name(self):
        """Sleeper's dictionary has no "DET-1" on the end of a name, so the
        suffix has to go or nothing resolves."""
        got = self.parse([row("RB2", "Bijan Robinson", "ATL", 5, 67, 86, 69)])
        self.assertEqual(got[0]["player"], "Bijan Robinson")


ARTICLE = """
<h3>Chase Brown</h3>, RB, CIN
<blockquote><p>Redraft ESPN ADP: 26.2 (RB13)</p>
<p>2025 Metrics: 8.4 Rec FPG (RB5), 1.3 Explosive FPG (RB34), 2.0 Inside-10 FPG (RB24)</p></blockquote>
<p>We&rsquo;re kicking this off with the most mispriced RB in fantasy football right now.
Some filler sentence ending in a price tag.</p>
<h3>Cam Skattebo</h3>, RB, NYG
<blockquote><p>Redraft ESPN ADP: 42.7 (RB18)</p>
<p>2025 Metrics: 7.1 Rec FPG (RB8), 1.0 Explosive FPG (RB40), 3.2 Inside-10 FPG (RB8)</p></blockquote>
"""


class ExtractFantasyPointsTest(unittest.TestCase):
    def parse(self, markup):
        with tempfile.NamedTemporaryFile("w", suffix=".html", delete=False) as f:
            f.write(markup)
            path = f.name
        try:
            return fp.extract(path)
        finally:
            os.unlink(path)

    def test_pulls_candidates_and_metrics(self):
        got = self.parse(ARTICLE)
        self.assertEqual(len(got), 2)
        brown = got[0]
        self.assertEqual(brown["player"], "Chase Brown")
        self.assertEqual(brown["position"], "RB")
        self.assertEqual(brown["team"], "CIN")
        self.assertEqual(brown["espn_adp"], "26.2")
        self.assertEqual((brown["rec_fpg"], brown["explosive_fpg"], brown["inside10_fpg"]), (8.4, 1.3, 2.0))

    def test_name_does_not_absorb_the_previous_sentence(self):
        """The heading sits after a paragraph ending in "a price tag." — a
        greedy backwards match turns that into the player's name."""
        got = self.parse(ARTICLE)
        for r in got:
            self.assertNotIn(".", r["player"])
            self.assertLessEqual(len(r["player"].split()), 3, r["player"])
        self.assertEqual(got[1]["player"], "Cam Skattebo")

    def test_counts_traits_against_the_articles_thresholds(self):
        got = {r["player"]: r for r in self.parse(ARTICLE)}
        # Brown clears receiving (8.4 >= 6.0) only.
        self.assertEqual(got["Chase Brown"]["traits_hit"], 1)
        self.assertEqual(got["Chase Brown"]["hits_receiving"], 1)
        self.assertEqual(got["Chase Brown"]["hits_explosive"], 0)
        # Skattebo clears receiving (7.1) and goal line (3.2 < 3.5 -> no).
        self.assertEqual(got["Cam Skattebo"]["hits_goal_line"], 0)

    def test_half_ppr_estimate_is_lower_than_full_ppr(self):
        """The article's receiving figures are full-PPR; this league is not."""
        got = {r["player"]: r for r in self.parse(ARTICLE)}
        brown = got["Chase Brown"]
        self.assertLess(brown["rec_fpg_half_ppr_est"], brown["rec_fpg"])
        self.assertGreater(brown["rec_fpg_half_ppr_est"], brown["rec_fpg"] * 0.5)

    def test_table_of_contents_entries_are_ignored(self):
        """The page lists every candidate in a TOC as well; only sections
        carrying ADP and metrics are real."""
        toc = '<ul><li><a href="#chase-brown-rb-cin">Chase Brown, RB, CIN</a></li></ul>' + ARTICLE
        self.assertEqual(len(self.parse(toc)), 2)


if __name__ == "__main__":
    unittest.main(verbosity=2)
