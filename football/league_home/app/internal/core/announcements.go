package core

import (
	"embed"
	"encoding/json"
	"sort"
	"time"
)

//go:embed data/announcements.json
var announcementsFS embed.FS

// Announcement is one league-wide announcement.
//
// This is placeholder/example data: the league currently posts
// announcements straight to Discord (see ../communication.md), so there's
// no real feed to transcribe yet. The schema exists so a future sync (a
// Discord bot reading a specific channel, or a small posting tool) has
// somewhere to write to.
type Announcement struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	PostedAt string `json:"posted_at"` // RFC3339
	Author   string `json:"author"`
}

// Announcements returns league announcements, most recent first.
func (s *Service) Announcements() ([]Announcement, error) {
	raw, err := announcementsFS.ReadFile("data/announcements.json")
	if err != nil {
		return nil, err
	}
	var a []Announcement
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	sort.Slice(a, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, a[i].PostedAt)
		tj, _ := time.Parse(time.RFC3339, a[j].PostedAt)
		return ti.After(tj)
	})
	return a, nil
}
