package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"

	"leaguehome/internal/core"
)

// commands mirrors leaguectl's command set 1:1, minus -league (a Discord
// bot is installed once per server for one league, configured via the
// LEAGUE_ID environment variable rather than a per-command flag).
var commands = []*discordgo.ApplicationCommand{
	{Name: "standings", Description: "Current league standings"},
	{Name: "faab", Description: "FAAB (waiver budget) balances"},
	{
		Name:        "matchups",
		Description: "Matchups for a given week",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "week",
				Description: "Week number",
				Required:    true,
				MinValue:    floatPtr(1),
			},
		},
	},
	{Name: "history", Description: "Award and league-role history"},
	{Name: "rules", Description: "Current roster/keeper/waiver/draft rules"},
	{Name: "scoring", Description: "Live scoring settings (from Sleeper)"},
	{Name: "managers", Description: "All managers, past and present"},
	{Name: "announcements", Description: "League announcements"},
	{Name: "schedule", Description: "Season calendar events"},
	{Name: "rivalries", Description: "Manager head-to-head records"},
	{Name: "state", Description: "Current NFL season/week"},
}

func floatPtr(f float64) *float64 { return &f }

// handleInteraction dispatches a slash command to its handler and replies
// with the formatted result, or an ephemeral error message on failure.
func handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, svc *core.Service) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()

	// scoring's output (~2KB across 7 categories) doesn't fit Discord's
	// 2000-char message content limit, so it gets its own embed (6000-char
	// limit, 1024 per field) with one field per category instead of a
	// single code block.
	if data.Name == "scoring" {
		embed, err := buildScoringEmbed(svc)
		if err != nil {
			log.Printf("scoring failed: %v", err)
			respondEphemeral(s, i, fmt.Sprintf("scoring failed: %v", err))
			return
		}
		respondEmbed(s, i, embed)
		return
	}

	var body string
	var err error

	switch data.Name {
	case "standings":
		body, err = formatStandings(svc)
	case "faab":
		body, err = formatFaab(svc)
	case "matchups":
		week := int(data.Options[0].IntValue())
		body, err = formatMatchups(svc, week)
	case "history":
		body, err = formatHistory(svc)
	case "rules":
		body, err = formatRules(svc)
	case "managers":
		body, err = formatManagers(svc)
	case "announcements":
		body, err = formatAnnouncements(svc)
	case "schedule":
		body, err = formatSchedule(svc)
	case "rivalries":
		body, err = formatRivalries(svc)
	case "state":
		body, err = formatState(svc)
	default:
		respondEphemeral(s, i, fmt.Sprintf("unknown command %q", data.Name))
		return
	}

	if err != nil {
		log.Printf("%s failed: %v", data.Name, err)
		respondEphemeral(s, i, fmt.Sprintf("%s failed: %v", data.Name, err))
		return
	}
	respond(s, i, body)
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, body string) {
	content := "```\n" + body + "\n```"
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
	if err != nil {
		log.Printf("responding to interaction: %v", err)
	}
}

func respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{embed}},
	})
	if err != nil {
		log.Printf("responding to interaction: %v", err)
	}
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Printf("responding to interaction: %v", err)
	}
}

func formatStandings(svc *core.Service) (string, error) {
	rows, err := svc.Standings()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, r := range rows {
		fmt.Fprintf(&b, "%2d. %-25s %d-%d-%d  PF %.2f  PA %.2f\n",
			i+1, r.Team, r.Wins, r.Losses, r.Ties, r.PointsFor, r.PointsAgainst)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatFaab(svc *core.Service) (string, error) {
	rows, err := svc.Faab()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%-25s remaining %3d / %3d (used %d)\n", r.Team, r.Remaining, r.Budget, r.Used)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatMatchups(svc *core.Service, week int) (string, error) {
	rows, err := svc.Matchups(week)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return fmt.Sprintf("No matchups for week %d yet.", week), nil
	}
	var b strings.Builder
	for _, m := range rows {
		fmt.Fprintf(&b, "%-25s %6.2f  vs  %6.2f  %s\n", m.Home, m.HomePoints, m.AwayPoints, m.Away)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatHistory(svc *core.Service) (string, error) {
	h, err := svc.History()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, a := range h.Awards {
		fmt.Fprintf(&b, "%d  Grand Champion: %-30s Sack-O: %s\n", a.Season, a.GrandChampion, a.SackO)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatRules(svc *core.Service) (string, error) {
	r, err := svc.Rules()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintln(&b, "Starting lineup:")
	for _, slot := range r.Roster.StartingSlots {
		fmt.Fprintf(&b, "  %-10s starters=%d max_on_roster=%d\n", slot.Position, slot.Starters, slot.MaxOnRoster)
	}
	fmt.Fprintf(&b, "Bench: %d  IR: %d\n\n", r.Roster.BenchSlots, r.Roster.IRSlots)

	fmt.Fprintf(&b, "Keepers: max %d, minimum value $%d, +$%d per keep count\n",
		r.Keepers.MaxKeepers, r.Keepers.MinimumValue, r.Keepers.IncrementPerKeepCount)
	fmt.Fprintf(&b, "Waivers: $%d budget, $%d minimum bid, %s\n",
		r.Waivers.YearlyBudget, r.Waivers.MinimumBid, r.Waivers.ProcessingSchedule)
	fmt.Fprintf(&b, "Draft: %s, $%d base budget\n", r.Draft.Format, r.Draft.BaseBudget)
	fmt.Fprintf(&b, "Trade deadline: start of week %d\n\n", r.TradeDeadlineWeek)

	fmt.Fprintln(&b, "Playoffs:")
	for _, p := range r.Playoffs {
		fmt.Fprintf(&b, "  %d-team league: weeks %d-%d, %d teams (%d byes)\n",
			p.LeagueSize, p.StartWeek, p.EndWeek, p.PlayoffTeams, p.ByeTeams)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func buildScoringEmbed(svc *core.Service) (*discordgo.MessageEmbed, error) {
	categories, err := svc.Scoring()
	if err != nil {
		return nil, err
	}
	embed := &discordgo.MessageEmbed{
		Title:  "Scoring settings",
		Fields: make([]*discordgo.MessageEmbedField, 0, len(categories)),
	}
	for _, c := range categories {
		var b strings.Builder
		for _, e := range c.Entries {
			fmt.Fprintf(&b, "%-40s %+g\n", e.Label, e.Points)
		}
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:  c.Name,
			Value: "```\n" + strings.TrimRight(b.String(), "\n") + "\n```",
		})
	}
	return embed, nil
}

func formatManagers(svc *core.Service) (string, error) {
	managers, err := svc.Managers()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, m := range managers {
		status := "active"
		if !m.Active {
			status = "inactive"
		}
		aliases := ""
		if len(m.Aliases) > 0 {
			aliases = fmt.Sprintf(" (aka %v)", m.Aliases)
		}
		fmt.Fprintf(&b, "%-20s %-9s%s\n", m.Name, status, aliases)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatAnnouncements(svc *core.Service) (string, error) {
	rows, err := svc.Announcements()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, a := range rows {
		fmt.Fprintf(&b, "[%s] %s - %s\n  %s\n", a.PostedAt, a.Title, a.Author, a.Body)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatSchedule(svc *core.Service) (string, error) {
	rows, err := svc.Schedule()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range rows {
		switch {
		case e.Recurring:
			fmt.Fprintf(&b, "%-20s (recurring)  %s\n", e.Label, e.Detail)
		case e.Week > 0:
			fmt.Fprintf(&b, "%-20s (week %d)    %s\n", e.Label, e.Week, e.Detail)
		default:
			fmt.Fprintf(&b, "%-20s              %s\n", e.Label, e.Detail)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatRivalries(svc *core.Service) (string, error) {
	rows, err := svc.Rivalries()
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "No rivalry data yet (needs live Sleeper history to compute).", nil
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s vs %s: %d-%d-%d  (PF %.2f vs %.2f)\n",
			r.ManagerAID, r.ManagerBID, r.WinsA, r.WinsB, r.Ties, r.PointsForA, r.PointsForB)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatState(svc *core.Service) (string, error) {
	st, err := svc.State()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("season %s (%s), week %d", st.Season, st.SeasonType, st.Week), nil
}
