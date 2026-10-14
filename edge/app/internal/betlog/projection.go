package betlog

import "time"

// Projection is defined fully in Task 2. This stub exists so betlog.go's
// struct fields compile before that task lands.
type Projection struct {
	Season   int
	Week     int
	Player   string
	Outcome  string
	Scenario string
	Kickoff  time.Time
}
