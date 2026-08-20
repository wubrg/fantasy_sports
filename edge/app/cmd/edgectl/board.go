package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"edge/internal/board"
)

// defaultBoardDir is where the per-week files live, relative to the repo root.
const defaultBoardDir = "../moneylines"

// defaultSchedule is the nflverse cache written by edge/model/ingest/nflverse.py.
const defaultSchedule = "../model/data/raw/games.csv"

func boardCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("board needs a mode: 'scaffold', 'validate', 'serve' or 'import'")
	}
	switch args[0] {
	case "scaffold":
		return boardScaffold(args[1:])
	case "validate":
		return boardValidate(args[1:])
	case "serve":
		return boardServe(args[1:])
	case "import":
		return boardImport(args[1:])
	default:
		return fmt.Errorf("unknown board mode %q (want 'scaffold', 'validate', 'serve' or 'import')", args[0])
	}
}

func boardScaffold(args []string) error {
	fs := flag.NewFlagSet("board scaffold", flag.ExitOnError)
	dir := fs.String("dir", defaultBoardDir, "directory holding the week files")
	sched := fs.String("schedule", defaultSchedule, "path to games.csv")
	season := fs.Int("season", 2026, "season to scaffold")
	week := fs.Int("week", 0, "only this week (0 = every week)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	f, err := os.Open(*sched)
	if err != nil {
		return fmt.Errorf("cannot read the schedule: %w\n"+
			"run `python3 edge/model/ingest/nflverse.py --seasons %d` to fetch it", err, *season)
	}
	defer f.Close()

	games, err := board.ReadSchedule(f, *season)
	if err != nil {
		return err
	}
	if len(games) == 0 {
		return fmt.Errorf("no %d regular-season games found in %s", *season, *sched)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}

	byWeek := board.SplitByWeek(games)
	weeks := make([]int, 0, len(byWeek))
	for w := range byWeek {
		if *week == 0 || w == *week {
			weeks = append(weeks, w)
		}
	}
	if len(weeks) == 0 {
		return fmt.Errorf("no games found for week %d", *week)
	}
	sort.Ints(weeks)

	totalGames, totalAdded := 0, 0
	for _, w := range weeks {
		path := filepath.Join(*dir, fmt.Sprintf("week%02d.yaml", w))
		doc, err := loadOrNew(path, *season, w)
		if err != nil {
			return err
		}
		added := doc.Scaffold(byWeek[w])
		if err := writeDoc(path, doc); err != nil {
			return err
		}
		totalGames += len(doc.Games)
		totalAdded += added
		fmt.Printf("week %02d: %2d games (%d new) -> %s\n", w, len(doc.Games), added, path)
	}
	fmt.Printf("\n%d games across %d week file(s); %d newly added, existing prices untouched.\n",
		totalGames, len(weeks), totalAdded)
	return nil
}

// loadOrNew reads an existing week file, or returns an empty document.
//
// A missing file is the normal first run. A malformed one is not, and must
// stop the run: scaffolding over a file that failed to parse would replace a
// board you have already typed into with a fresh empty one.
func loadOrNew(path string, season, week int) (*board.Doc, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return &board.Doc{Season: season, Week: week, Games: map[string]*board.Game{}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	doc, err := board.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s is not readable, refusing to overwrite it: %w", path, err)
	}
	doc.Season, doc.Week = season, week
	return doc, nil
}

// writeDoc writes through a temporary file so an interrupted run cannot leave
// a half-written board behind.
func writeDoc(path string, doc *board.Doc) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := doc.Encode(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func boardValidate(args []string) error {
	fs := flag.NewFlagSet("board validate", flag.ExitOnError)
	dir := fs.String("dir", defaultBoardDir, "directory holding the week files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	paths, err := filepath.Glob(filepath.Join(*dir, "week*.yaml"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no week files in %s -- run `edgectl board scaffold` first", *dir)
	}
	sort.Strings(paths)

	total, bad := 0, 0
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		doc, err := board.Parse(f)
		f.Close()
		if err != nil {
			fmt.Printf("%s: %v\n", p, err)
			bad++
			continue
		}
		problems := doc.Validate()
		total += len(doc.Games)
		for _, pr := range problems {
			fmt.Printf("%s: %s\n", p, pr)
			bad++
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d problem(s) across %d games", bad, total)
	}
	fmt.Printf("%d games across %d week file(s): every price parses.\n", total, len(paths))
	return nil
}
