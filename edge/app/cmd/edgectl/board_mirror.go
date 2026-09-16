package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"edge/internal/board"
)

// boardMirror copies one book's prices into another for a week -- the one-time
// bridge for going DK-only when boards were priced on the consensus column.
// It never overwrites a non-empty destination cell and never clears a source.
func boardMirror(args []string) error {
	fs := flag.NewFlagSet("board mirror", flag.ContinueOnError)
	dir := fs.String("dir", defaultBoardDir, "directory holding the week files")
	week := fs.Int("week", 0, "NFL week to mirror (required)")
	from := fs.String("from", board.Consensus, "source book to copy from")
	to := fs.String("to", board.DefaultBook, "destination book to copy into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *week <= 0 {
		return fmt.Errorf("-week is required")
	}

	// Load exactly as board_import.go does: open week%02d.yaml and board.Parse it.
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

	copied := 0
	for _, gid := range doc.GameIDs() {
		src := doc.Games[gid].Books[*from]
		dst := doc.Games[gid].Books[*to]
		for _, m := range []struct {
			name, srcVal, dstVal string
		}{
			{"ml", src.ML, dst.ML},
			{"spread", src.Spread, dst.Spread},
			{"total", src.Total, dst.Total},
		} {
			if m.srcVal == "" || m.dstVal != "" { // nothing to copy, or don't clobber
				continue
			}
			if err := doc.SetPrice(gid, *to, m.name, m.srcVal); err != nil {
				return fmt.Errorf("%s %s: %w", gid, m.name, err)
			}
			copied++
		}
	}

	if err := writeDoc(path, doc); err != nil { // writeDoc: board.go:124
		return err
	}
	fmt.Printf("mirrored %d cells from %s to %s for week %d\n", copied, *from, *to, *week)
	return nil
}
