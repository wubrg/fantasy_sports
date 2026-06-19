// Command nflctl is the maintenance CLI for the NFL awards SQLite database:
// importing the legacy JSON snapshot, adding/removing rows with validation,
// quick filtered lookups, and exporting a fresh JSON snapshot for backups
// and git-diffable history.
package main

import (
	"flag"
	"fmt"
	"os"

	"nflawards/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "import":
		cmdImport(args)
	case "add":
		cmdAdd(args)
	case "rm":
		cmdRemove(args)
	case "list":
		cmdList(args)
	case "export-json":
		cmdExport(args)
	case "-h", "-help", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `nflctl - maintain the NFL awards database

Usage:
  nflctl import -db PATH JSON_FILE        Bulk-load award rows from a legacy JSON export
  nflctl add -db PATH -year Y -player P -pos POS -unit U -team T -award A [-notes N]
                                           Add one validated award row
  nflctl rm -db PATH -id ID               Remove a row by id
  nflctl list -db PATH [-year Y] [-team T] [-award A] [-player P]
                                           List rows matching filters
  nflctl export-json -db PATH OUT_FILE    Write the current dataset to a JSON file

Flags default -db to ../data/nfl_awards.db (run from the app/ directory).
Valid -unit values: O, D, ST.
`)
}

func cmdImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dbPath := fs.String("db", "../data/nfl_awards.db", "path to sqlite db")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: nflctl import -db PATH JSON_FILE")
		os.Exit(1)
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer s.Close()

	inserted, skipped, err := s.ImportJSON(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("imported %d rows, skipped %d duplicates\n", inserted, skipped)
}

func cmdAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	dbPath := fs.String("db", "../data/nfl_awards.db", "path to sqlite db")
	year := fs.Int("year", 0, "season year")
	player := fs.String("player", "", "player name")
	pos := fs.String("pos", "", "position code")
	unit := fs.String("unit", "", "O, D, or ST")
	team := fs.String("team", "", "team code, e.g. KC")
	award := fs.String("award", "", "award code, e.g. \"AP MVP\" or \"Pro Bowl\"")
	notes := fs.String("notes", "", "optional notes")
	fs.Parse(args)

	if *year == 0 || *player == "" || *pos == "" || *unit == "" || *team == "" || *award == "" {
		fmt.Fprintln(os.Stderr, "missing required flag: -year -player -pos -unit -team -award are all required")
		os.Exit(1)
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer s.Close()

	id, err := s.Add(store.Row{
		Year: *year, Player: *player, Pos: *pos, Unit: *unit,
		Team: *team, Award: *award, Notes: *notes,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "add failed (check team/unit/award codes are valid, and this isn't a duplicate): %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("added row id=%d\n", id)
}

func cmdRemove(args []string) {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	dbPath := fs.String("db", "../data/nfl_awards.db", "path to sqlite db")
	id := fs.Int64("id", 0, "row id to remove")
	fs.Parse(args)

	if *id == 0 {
		fmt.Fprintln(os.Stderr, "missing required flag: -id")
		os.Exit(1)
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer s.Close()

	if err := s.Remove(*id); err != nil {
		fmt.Fprintf(os.Stderr, "remove failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("removed row id=%d\n", *id)
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dbPath := fs.String("db", "../data/nfl_awards.db", "path to sqlite db")
	year := fs.Int("year", 0, "filter by year")
	team := fs.String("team", "", "filter by team code")
	award := fs.String("award", "", "filter by award code")
	player := fs.String("player", "", "filter by player substring")
	fs.Parse(args)

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer s.Close()

	rows, err := s.List(store.Filter{Year: *year, Team: *team, Award: *award, Player: *player})
	if err != nil {
		fmt.Fprintf(os.Stderr, "list failed: %v\n", err)
		os.Exit(1)
	}
	for _, r := range rows {
		fmt.Printf("id=%-5d %d  %-25s %-8s %-3s %-3s %-12s %s\n",
			r.ID, r.Year, r.Player, r.Pos, r.Unit, r.Team, r.Award, r.Notes)
	}
	fmt.Printf("%d row(s)\n", len(rows))
}

func cmdExport(args []string) {
	fs := flag.NewFlagSet("export-json", flag.ExitOnError)
	dbPath := fs.String("db", "../data/nfl_awards.db", "path to sqlite db")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: nflctl export-json -db PATH OUT_FILE")
		os.Exit(1)
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer s.Close()

	if err := s.ExportJSON(fs.Arg(0)); err != nil {
		fmt.Fprintf(os.Stderr, "export failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("exported dataset to %s\n", fs.Arg(0))
}
