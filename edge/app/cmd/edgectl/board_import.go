package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"edge/internal/board"
)

// boardImport backfills a week's moneylines from a blob copied off a book.
//
// This is the same code path the desktop paste panel uses, exposed as a
// command so historical weeks can be filled in without a browser. It is not
// the primary way prices get entered -- that is `board serve` on a phone,
// where there is nothing to copy from without app-switching.
func boardImport(args []string) error {
	flags := flag.NewFlagSet("board import", flag.ExitOnError)
	dir := flags.String("dir", defaultBoardDir, "directory holding the week files")
	week := flags.Int("week", 0, "week to import into (required)")
	book := flags.String("book", "", "book to write, e.g. fanatics (required)")
	file := flags.String("file", "", "file holding the blob (default: stdin)")
	dry := flags.Bool("n", false, "show the diff and write nothing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *week <= 0 {
		return fmt.Errorf("-week is required")
	}
	if *book == "" {
		return fmt.Errorf("-book is required (one of: %v)", board.Books)
	}

	blob, err := readBlob(*file)
	if err != nil {
		return err
	}
	pairs, err := board.ParsePaste(blob)
	if err != nil {
		return err
	}

	path := filepath.Join(*dir, fmt.Sprintf("week%02d.yaml", *week))
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	doc, err := board.Parse(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s is not readable, refusing to write to it: %w", path, err)
	}

	// Planning happens before anything is written, and a single unmatched pair
	// stops the whole import: a blob that lost one entry shifts every pair
	// after it and would otherwise write plausible-looking prices onto the
	// wrong games.
	changes, err := doc.PlanImport(pairs, *book, "ml")
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Printf("week %02d %s: nothing to change; these prices are already on the board.\n", *week, *book)
		return nil
	}
	for _, c := range changes {
		fmt.Println(c)
	}
	if *dry {
		fmt.Printf("\n%d change(s) NOT written (-n).\n", len(changes))
		return nil
	}
	if err := doc.ApplyImport(changes); err != nil {
		return err
	}
	if err := writeDoc(path, doc); err != nil {
		return err
	}
	fmt.Printf("\n%d change(s) written to %s.\n", len(changes), path)
	return nil
}

func readBlob(path string) (string, error) {
	if path == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
