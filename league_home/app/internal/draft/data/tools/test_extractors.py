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
import csv  # noqa: E402
import extract_fantasypoints as fp  # noqa: E402
import extract_fantasypros as fpx  # noqa: E402
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


# --- FantasyPros ------------------------------------------------------------
#
# The trap here is the opposite of Subvertadown's: nothing is hidden in one
# cell, but one player is spread across four separate CSV views that must be
# merged, and the stats view — the only one carrying the numbers — has no POS
# column to merge on. On top of that the point total has to be recomputed
# under league scoring from components that omit interceptions.

# stats view: RK,TIERS,NAME,TEAM,FANTASYPTS, then pass(yds,td), rec(rec,yds,td),
# rush(att,yds,td) — the three YDS/TDS pairs repeat by name.
FPX_STATS_HEADER = ['RK', 'TIERS', 'PLAYER NAME', 'TEAM', 'FANTASYPTS',
                    'YDS', 'TDS', 'REC', 'YDS', 'TDS', 'ATT', 'YDS', 'TDS']


def fpx_write(path, header, rows):
    with open(path, 'w', newline='', encoding='utf-8') as f:
        w = csv.writer(f)
        w.writerow(header)
        w.writerows(rows)


def fpx_variant(raw_dir, prefix, players):
    """Write the four view files for one variant. Each player is a dict with
    rank, tier, name, team, pos, bye, note, and a stats 9-tuple
    (fpts, pyds, ptds, rec, ryds, rtds, att, ruyds, rutds)."""
    ov = [[p['rank'], p['tier'], p['name'], p['team'], p['pos'], p['bye'],
           '5 out of 5', '2 out of 5', '3 out of 5 stars', p.get('adp', '-')]
          for p in players]
    fpx_write(os.path.join(raw_dir, f'{prefix}-ecr-for-2026.csv'),
              ['RK', 'TIERS', 'PLAYER NAME', 'TEAM', 'POS', 'BYE WEEK',
               'UPSIDE ', 'BUST ', 'SOS SEASON', 'ECR VS. ADP'], ov)
    rk = [[p['rank'], p['tier'], p['name'], p['team'], p['pos'],
           p['rank'], p['rank'] + 4, float(p['rank']) + 0.5, 0.8, p.get('adp', '-')]
          for p in players]
    fpx_write(os.path.join(raw_dir, f'{prefix}-ecr-ranks-for-2026.csv'),
              ['RK', 'TIERS', 'PLAYER NAME', 'TEAM', 'POS', 'BEST', 'WORST',
               'AVG.', 'STD.DEV', 'ECR VS. ADP'], rk)
    st = [[p['rank'], p['tier'], p['name'], p['team'], *p['stats']] for p in players]
    fpx_write(os.path.join(raw_dir, f'{prefix}-ecr-stats-for-2026.csv'),
              FPX_STATS_HEADER, st)
    nt = [[p['rank'], p['tier'], p['name'], p['team'], p['pos'], p['bye'],
           p.get('note', '')] for p in players]
    fpx_write(os.path.join(raw_dir, f'{prefix}-ecr-notes-for-2026.csv'),
              ['RK', 'TIERS', 'PLAYER NAME', 'TEAM', 'POS', 'BYE WEEK', 'NOTES'], nt)


FPX_QB_PROJ_HEADER = ['Player', 'Team', 'ATT', 'CMP', 'YDS', 'TDS', 'INTS',
                      'ATT', 'YDS', 'TDS', 'FL', 'FPTS']
FPX_FLX_PROJ_HEADER = ['Player', 'Team', 'POS', 'ATT', 'YDS', 'TDS',
                       'REC', 'YDS', 'TDS', 'FL', 'FPTS']


def fpx_projections(raw_dir, players):
    """Write the two Hi/Low projection exports.

    A player with a 'proj' key contributes three consecutive rows: the named
    average, then an unnamed 'high', then an unnamed 'low'. FantasyPros also
    writes a blank sub-header beneath the real header, reproduced here because
    the parser has to survive it.
    """
    qb, flx = [], []
    for p in players:
        if 'proj' not in p:
            continue
        if p['pos'].startswith('QB'):
            qb.append([p['name'], p['team'], *p['proj']])
            qb.append(['', 'high', *p['proj_high']])
            qb.append(['', 'low', *p['proj_low']])
        else:
            flx.append([p['name'], p['team'], p['pos'], *p['proj']])
            flx.append(['', 'high', '', *p['proj_high']])
            flx.append(['', 'low', '', *p['proj_low']])
    fpx_write(os.path.join(raw_dir, 'fantasypros-projections-qb-hilo-for-2026.csv'),
              FPX_QB_PROJ_HEADER, [[' ', '', '', '']] + qb)
    fpx_write(os.path.join(raw_dir, 'fantasypros-projections-flx-hilo-for-2026.csv'),
              FPX_FLX_PROJ_HEADER, [[' ', '', ' ', '', '']] + flx)


# Gibbs and Allen carry the two scoring cases; the consensus set also has a
# kicker (dropped), the Jaguars DST (team JAC -> JAX, its own number stands),
# and a deep player FantasyPros ranks but does not project.
#
# `stats` is the ECR stats view — last season's actuals, kept only because a
# DST has nothing else. `proj` is the projection export, which is what scoring
# now reads: flex is (rush att, yds, td, rec, yds, td, FL, FPTS) and QB is
# (att, cmp, yds, td, INT, rush att, yds, td, FL, FPTS).
GIBBS = {'rank': 1, 'tier': 1, 'name': 'Jahmyr Gibbs', 'team': 'DET', 'pos': 'RB1',
         'bye': 6, 'note': 'Workhorse back.', 'stats': (328.4, 0, 0, 77, 616, 5, 243, 1223, 13),
         'proj': (243, 1223, 13, 77, 616, 5, 1.0, 328.4),
         'proj_high': (250, 1300, 15, 80, 650, 6, 1.0, 360),
         'proj_low': (235, 1150, 11, 72, 580, 4, 1.0, 300)}
ALLEN = {'rank': 26, 'tier': 3, 'name': 'Josh Allen', 'team': 'BUF', 'pos': 'QB1',
         'bye': 7, 'stats': (374.6, 3668, 25, 0, 0, 0, 112, 579, 14),
         'proj': (550, 380, 3668, 25, 12, 112, 579, 14, 3.0, 374.6),
         'proj_high': (560, 390, 3900, 28, 10, 118, 620, 15, 3.0, 400),
         'proj_low': (540, 370, 3400, 22, 15, 105, 540, 12, 3.0, 350)}
BIJAN = {'rank': 2, 'tier': 1, 'name': 'Bijan Robinson', 'team': 'ATL', 'pos': 'RB2',
         'bye': 5, 'stats': (331.3, 0, 0, 60, 500, 4, 260, 1350, 12),
         'proj': (260, 1350, 12, 60, 500, 4, 1.5, 331.3),
         'proj_high': (270, 1420, 14, 64, 540, 5, 1.5, 360),
         'proj_low': (250, 1280, 10, 56, 470, 3, 1.5, 305)}
KICKER = {'rank': 150, 'tier': 8, 'name': 'Cameron Dicker', 'team': 'LAC', 'pos': 'K1',
          'bye': 12, 'stats': (150, 0, 0, 0, 0, 0, 0, 0, 0)}
JAGS = {'rank': 170, 'tier': 8, 'name': 'Jacksonville Jaguars', 'team': 'JAC', 'pos': 'DST1',
        'bye': 8, 'stats': (120, 0, 0, 0, 0, 0, 0, 0, 0)}
# Ranked, never projected — the case that used to floor at zero and read as a
# player who scores nothing.
DEEP = {'rank': 400, 'tier': 14, 'name': 'Deep Reserve', 'team': 'NYJ', 'pos': 'RB80',
        'bye': 9, 'stats': (0, 0, 0, 0, 0, 0, 0, 0, 0)}


class ExtractFantasyProsTest(unittest.TestCase):
    def build(self):
        """A raw dir with all three variants. Consensus ranks Gibbs over Bijan;
        the sharp subsets flip it, which is the divergence signal."""
        d = tempfile.mkdtemp()
        everyone = [GIBBS, BIJAN, ALLEN, KICKER, JAGS, DEEP]
        fpx_variant(d, 'fantasypros', everyone)
        # top10/top20 rank Bijan #1, Gibbs #2 — the flip.
        b1 = dict(BIJAN, rank=1)
        g2 = dict(GIBBS, rank=2)
        fpx_variant(d, 'fantasypros-2025-top10', [b1, g2, dict(ALLEN, rank=24)])
        fpx_variant(d, 'fantasypros-2025-top20', [b1, g2, dict(ALLEN, rank=25)])
        # Projections are one set for every variant: how well a player is
        # expected to do does not depend on which experts ranked him.
        fpx_projections(d, everyone)
        return d

    def consensus(self, raw_dir=None):
        d = raw_dir or self.build()
        return fpx.parse_variant(d, 'fantasypros', fpx.parse_projections(d))

    def test_views_merge_by_name(self):
        p = self.consensus()['Jahmyr Gibbs']
        self.assertEqual(p['position'], 'RB')
        self.assertEqual(p['pos_rank'], 1)
        self.assertEqual(p['overall_rank'], 1)
        self.assertEqual(p['best'], 1)          # from the ranks view
        self.assertAlmostEqual(p['avg_rank'], 1.5)  # from the ranks view
        self.assertEqual(p['receptions'], 77.0)  # from the projections export

    def test_league_recompute_matches_for_skill_players(self):
        p = self.consensus()['Jahmyr Gibbs']
        # 77*.5 + 616*.1 + 5*6 + 1223*.1 + 13*6 = 330.4
        self.assertAlmostEqual(p['league_points'], 330.4, places=1)
        self.assertAlmostEqual(p['points_delta'], 2.0, places=1)

    def test_qb_interceptions_are_published_not_modelled(self):
        p = self.consensus()['Josh Allen']
        # 3668*.04 + 25*4 + 579*.1 + 14*6 = 388.62, less the 12 picks the
        # projection actually publishes at -1 each = 376.62. Nothing estimated.
        self.assertEqual(p['interceptions'], 12.0)
        self.assertAlmostEqual(p['league_points'], 376.62, places=2)
        self.assertLess(abs(p['points_delta']), 5)

    def test_skill_players_carry_no_interceptions(self):
        p = self.consensus()['Jahmyr Gibbs']
        self.assertEqual(p['interceptions'], 0.0)

    def test_band_is_recomputed_under_league_scoring(self):
        p = self.consensus()['Jahmyr Gibbs']
        # high: 1300*.1 + 15*6 + 80*.5 + 650*.1 + 6*6 = 361.0
        # low:  1150*.1 + 11*6 + 72*.5 + 580*.1 + 4*6 = 299.0
        self.assertAlmostEqual(p['points_high'], 361.0, places=1)
        self.assertAlmostEqual(p['points_low'], 299.0, places=1)
        self.assertLess(p['points_low'], p['league_points'])
        self.assertGreater(p['points_high'], p['league_points'])

    def test_a_passers_low_carries_more_interceptions_not_fewer(self):
        """The reason the band is recomputed rather than scaled from their
        published total: the pessimistic case throws more picks, and only
        recomputation under our scoring sees that."""
        p = self.consensus()['Josh Allen']
        # high: 3900*.04 + 28*4 + 620*.1 + 15*6 - 10 = 410.0
        # low:  3400*.04 + 22*4 + 540*.1 + 12*6 - 15 = 335.0
        self.assertAlmostEqual(p['points_high'], 410.0, places=1)
        self.assertAlmostEqual(p['points_low'], 335.0, places=1)

    def test_fumbles_are_recorded_but_never_scored(self):
        """SCORING has to stay key-for-key with Ciely, who has no fumble term,
        or FP stops being comparable to Value."""
        self.assertNotIn('fumbles_lost', fpx.SCORING)
        p = self.consensus()['Josh Allen']
        self.assertEqual(p['fumbles_lost'], 3.0)
        # 3 fumbles at any weight would have moved this; it did not.
        self.assertAlmostEqual(p['league_points'], 376.62, places=2)

    def test_scoring_keys_match_ciely(self):
        import extract_ciely as cix
        self.assertEqual(len(fpx.SCORING), len(cix.SCORING))
        self.assertIn('interceptions', fpx.SCORING)
        self.assertEqual(fpx.SCORING['interceptions'], cix.SCORING['INT'])

    def test_ranked_but_unprojected_player_has_no_points(self):
        """Empty, not zero. Zero reads as a player who scores nothing and
        floors him at a dollar on the board; empty reads as no FP opinion."""
        p = self.consensus()['Deep Reserve']
        self.assertIsNone(p['league_points'])
        self.assertIsNone(p['fantasypros_points'])
        self.assertIsNone(p['points_delta'])
        self.assertIsNone(p['points_high'])

    def test_kicker_is_dropped(self):
        self.assertNotIn('Cameron Dicker', self.consensus())

    def test_defense_team_is_normalized_and_its_number_stands(self):
        p = self.consensus()['Jacksonville Jaguars']
        self.assertEqual(p['team'], 'JAX')                 # JAC -> JAX
        self.assertEqual(p['league_points'], 120.0)        # published number stands
        self.assertEqual(p['points_delta'], 0.0)

    def out_rows(self):
        d = self.build()
        out = os.path.join(d, 'fantasypros-2026.csv')
        old = sys.argv
        sys.argv = ['extract_fantasypros.py', d, out]
        try:
            fpx.main()
        finally:
            sys.argv = old
        with open(out, encoding='utf-8') as f:
            return list(csv.DictReader(f))

    def test_divergence_lives_on_the_consensus_spine(self):
        rows = self.out_rows()
        con = {r['player']: r for r in rows if r['baseline'] == 'consensus'}
        # Sharps rank Bijan #1 over consensus #2: 2 - 1 = +1 (they rate him higher).
        self.assertEqual(con['Bijan Robinson']['rank_vs_top10'], '1')
        self.assertEqual(con['Bijan Robinson']['rank_vs_top20'], '1')
        # Gibbs the reverse: consensus #1, sharps #2 -> 1 - 2 = -1.
        self.assertEqual(con['Jahmyr Gibbs']['rank_vs_top10'], '-1')
        # The subset rows themselves carry no divergence.
        subs = [r for r in rows if r['baseline'] == 'top10']
        self.assertTrue(all(r['rank_vs_top10'] == '' for r in subs))

    def test_notes_ride_on_consensus_only(self):
        rows = self.out_rows()
        con = {r['player']: r for r in rows if r['baseline'] == 'consensus'}
        self.assertEqual(con['Jahmyr Gibbs']['notes'], 'Workhorse back.')
        top10 = [r for r in rows if r['baseline'] == 'top10']
        self.assertTrue(all(r['notes'] == '' for r in top10))


if __name__ == "__main__":
    unittest.main(verbosity=2)
