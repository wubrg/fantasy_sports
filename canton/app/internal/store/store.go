// Package store provides the SQLite-backed persistence layer for the NFL
// awards dataset. Reference tables (teams, units, award_types) carry the
// fixed taxonomy and enforce valid values on every award row via foreign
// keys; the awards table itself is the growable, user-editable data.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// Row mirrors one award row. JSON tags match the legacy flat-file schema
// (yr/pl/pos/u/tm/aw/nt) so the API and JSON export stay compatible with
// the existing frontend and any external tooling.
type Row struct {
	ID     int64  `json:"-"`
	Year   int    `json:"yr"`
	Player string `json:"pl"`
	Pos    string `json:"pos"`
	Unit   string `json:"u"`
	Team   string `json:"tm"`
	Award  string `json:"aw"`
	Notes  string `json:"nt"`
}

type Filter struct {
	Year   int    // 0 = any
	Team   string // "" = any
	Award  string // "" = any
	Player string // "" = any, substring match
}

// teamSeed, unitSeed, and awardSeed are the fixed taxonomy from ADR-001 /
// the UI spec. They rarely change; adding a relocated/renamed franchise is
// a deliberate code change here, not a data edit.
var teamSeed = map[string]string{
	"ARI": "Arizona Cardinals", "ATL": "Atlanta Falcons", "BAL": "Baltimore Ravens",
	"BUF": "Buffalo Bills", "CAR": "Carolina Panthers", "CHI": "Chicago Bears",
	"CIN": "Cincinnati Bengals", "CLE": "Cleveland Browns", "DAL": "Dallas Cowboys",
	"DEN": "Denver Broncos", "DET": "Detroit Lions", "GB": "Green Bay Packers",
	"HOU": "Houston Texans", "IND": "Indianapolis Colts", "JAX": "Jacksonville Jaguars",
	"KC": "Kansas City Chiefs", "LAC": "LA Chargers", "LAR": "LA Rams",
	"LV": "Las Vegas Raiders", "MIA": "Miami Dolphins", "MIN": "Minnesota Vikings",
	"NE": "New England Patriots", "NO": "New Orleans Saints", "NYG": "New York Giants",
	"NYJ": "New York Jets", "PHI": "Philadelphia Eagles", "PIT": "Pittsburgh Steelers",
	"SEA": "Seattle Seahawks", "SF": "San Francisco 49ers", "TB": "Tampa Bay Buccaneers",
	"TEN": "Tennessee Titans", "WAS": "Washington Commanders",
}

var unitSeed = map[string]string{
	"O": "Offense", "D": "Defense", "ST": "Special Teams",
}

// awardOrder fixes display/listing order to match the original taxonomy.
// The AFL-* and All-AFL codes are only valid for AFL teams in 1960-1969, the
// one decade the AFL operated as a separate league from the NFL before the
// merger — see ADR-002 for which codes apply in which years.
var awardOrder = []string{
	"AP MVP", "AP OPOY", "AP DPOY", "AP OROTY", "AP DROTY", "AP CPOTY",
	"SB MVP", "All-Pro 1st", "All-Pro 2nd", "Pro Bowl",
	"AFL MVP", "AFL ROY", "All-AFL 1st", "All-AFL 2nd", "AFL All-Star",
}

var awardSeed = map[string]string{
	"AP MVP":       "AP NFL Most Valuable Player",
	"AP OPOY":      "AP NFL Offensive Player of the Year",
	"AP DPOY":      "AP NFL Defensive Player of the Year",
	"AP OROTY":     "AP NFL Offensive Rookie of the Year",
	"AP DROTY":     "AP NFL Defensive Rookie of the Year",
	"AP CPOTY":     "AP NFL Comeback Player of the Year",
	"SB MVP":       "Super Bowl Most Valuable Player",
	"All-Pro 1st":  "AP All-Pro First Team",
	"All-Pro 2nd":  "AP All-Pro Second Team",
	"Pro Bowl":     "Pro Bowl / Pro Bowl Games selection",
	"AFL MVP":      "AFL Most Valuable Player / Player of the Year (1960-1969)",
	"AFL ROY":      "AFL Rookie of the Year (1960-1966, before the AP's O/D split)",
	"All-AFL 1st":  "All-AFL Team First Team (1960-1969)",
	"All-AFL 2nd":  "All-AFL Team Second Team (1960-1969)",
	"AFL All-Star": "AFL All-Star Game selection (1960-1969)",
}

const verifyNote = "Entries with [verify] in 'nt' field are Pro Bowl entries needing PFR cross-check"

const schema = `
CREATE TABLE IF NOT EXISTS teams (
	code TEXT PRIMARY KEY,
	name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS units (
	code TEXT PRIMARY KEY,
	name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS award_types (
	code TEXT PRIMARY KEY,
	name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS awards (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	yr INTEGER NOT NULL,
	player TEXT NOT NULL,
	pos TEXT NOT NULL,
	unit TEXT NOT NULL REFERENCES units(code),
	team TEXT NOT NULL REFERENCES teams(code),
	award TEXT NOT NULL REFERENCES award_types(code),
	notes TEXT NOT NULL DEFAULT '',
	UNIQUE(yr, player, pos, team, award)
);
CREATE INDEX IF NOT EXISTS idx_awards_year ON awards(yr);
CREATE INDEX IF NOT EXISTS idx_awards_team ON awards(team);
CREATE INDEX IF NOT EXISTS idx_awards_award ON awards(award);
`

// Open creates the database file if needed, applies the schema, and seeds
// the reference tables. Safe to call every time the app or CLI starts.
func Open(path string) (*Store, error) {
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(wal)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	s := &Store{db: db}
	if err := s.seedReferenceTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("seeding reference tables: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) seedReferenceTables() error {
	for code, name := range teamSeed {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO teams(code, name) VALUES (?, ?)`, code, name); err != nil {
			return err
		}
	}
	for code, name := range unitSeed {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO units(code, name) VALUES (?, ?)`, code, name); err != nil {
			return err
		}
	}
	for code, name := range awardSeed {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO award_types(code, name) VALUES (?, ?)`, code, name); err != nil {
			return err
		}
	}
	return nil
}

// Add validates and inserts one award row. Invalid team/unit/award codes
// are rejected by foreign key constraints; exact duplicates are rejected
// by the unique constraint. Both surface as clear errors.
func (s *Store) Add(r Row) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO awards(yr, player, pos, unit, team, award, notes) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Year, r.Player, r.Pos, r.Unit, r.Team, r.Award, r.Notes,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) Remove(id int64) error {
	res, err := s.db.Exec(`DELETE FROM awards WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no row with id %d", id)
	}
	return nil
}

func (s *Store) List(f Filter) ([]Row, error) {
	q := `SELECT id, yr, player, pos, unit, team, award, notes FROM awards WHERE 1=1`
	var args []any
	if f.Year != 0 {
		q += ` AND yr = ?`
		args = append(args, f.Year)
	}
	if f.Team != "" {
		q += ` AND team = ?`
		args = append(args, f.Team)
	}
	if f.Award != "" {
		q += ` AND award = ?`
		args = append(args, f.Award)
	}
	if f.Player != "" {
		q += ` AND player LIKE ?`
		args = append(args, "%"+f.Player+"%")
	}
	q += ` ORDER BY yr DESC, player`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.Year, &r.Player, &r.Pos, &r.Unit, &r.Team, &r.Award, &r.Notes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Meta describes the dataset shape, matching the legacy flat-file format.
type Meta struct {
	Rows       int               `json:"rows"`
	Years      [2]int            `json:"years"`
	Teams      []string          `json:"teams"`
	TeamNames  map[string]string `json:"team_names"`
	Awards     []string          `json:"awards"`
	Units      map[string]string `json:"units"`
	VerifyNote string            `json:"verify_note"`
}

type Dataset struct {
	Meta Meta  `json:"meta"`
	Data []Row `json:"data"`
}

// Dataset builds the full {meta, data} payload from the current DB state,
// in the same shape as the original canton_data.json, for the web API
// and for JSON export/backup.
func (s *Store) Dataset() (Dataset, error) {
	rows, err := s.List(Filter{})
	if err != nil {
		return Dataset{}, err
	}

	var minYr, maxYr int
	if err := s.db.QueryRow(`SELECT COALESCE(MIN(yr),0), COALESCE(MAX(yr),0) FROM awards`).Scan(&minYr, &maxYr); err != nil {
		return Dataset{}, err
	}

	teamNames := map[string]string{}
	teamRows, err := s.db.Query(`SELECT code, name FROM teams`)
	if err != nil {
		return Dataset{}, err
	}
	for teamRows.Next() {
		var code, name string
		if err := teamRows.Scan(&code, &name); err != nil {
			teamRows.Close()
			return Dataset{}, err
		}
		teamNames[code] = name
	}
	teamRows.Close()

	teams := make([]string, 0, len(teamNames))
	for code := range teamNames {
		teams = append(teams, code)
	}
	sort.Strings(teams)

	units := map[string]string{}
	for code, name := range unitSeed {
		units[code] = name
	}

	return Dataset{
		Meta: Meta{
			Rows:       len(rows),
			Years:      [2]int{minYr, maxYr},
			Teams:      teams,
			TeamNames:  teamNames,
			Awards:     awardOrder,
			Units:      units,
			VerifyNote: verifyNote,
		},
		Data: rows,
	}, nil
}

// ImportJSON bulk-loads award rows from a legacy flat-file JSON export.
// Duplicates (by the unique constraint) are skipped, not errors.
func (s *Store) ImportJSON(path string) (inserted, skipped int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	var legacy struct {
		Data []Row `json:"data"`
	}
	if err := json.NewDecoder(f).Decode(&legacy); err != nil {
		return 0, 0, fmt.Errorf("decoding %s: %w", path, err)
	}

	for _, r := range legacy.Data {
		if _, err := s.Add(r); err != nil {
			skipped++
			continue
		}
		inserted++
	}
	return inserted, skipped, nil
}

// ExportJSON writes the current dataset to disk in the legacy flat-file
// shape, for backups and human-readable git diffs.
func (s *Store) ExportJSON(path string) error {
	ds, err := s.Dataset()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
