// Command leaguebot is a Discord bot front end onto the league_home core
// data layer: the same Standings/Faab/Matchups/... operations leaguectl
// exposes over a CLI, exposed here as Discord slash commands. It owns no
// league-data logic of its own; every command just calls into
// internal/core and formats the result for Discord.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"

	"leaguehome/internal/core"
)

// defaultLeagueID is the "Hit or Miss" league's current Sleeper league ID
// (see leagues/hit_or_miss/readme.md). Override with the LEAGUE_ID environment
// variable for a different season or league.
const defaultLeagueID = "1368649414419189760"

func main() {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_BOT_TOKEN environment variable is required")
	}

	leagueID := os.Getenv("LEAGUE_ID")
	if leagueID == "" {
		leagueID = defaultLeagueID
	}
	svc := core.New(leagueID)

	// DISCORD_GUILD_ID scopes slash-command registration to one server,
	// where Discord propagates the commands within seconds instead of the
	// up-to-an-hour delay for global commands. Leave unset once the bot is
	// only ever installed in the one league server.
	guildID := os.Getenv("DISCORD_GUILD_ID")

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("creating Discord session: %v", err)
	}

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		handleInteraction(s, i, svc)
	})

	if err := session.Open(); err != nil {
		log.Fatalf("opening Discord session: %v", err)
	}
	defer session.Close()

	registered, err := session.ApplicationCommandBulkOverwrite(session.State.User.ID, guildID, commands)
	if err != nil {
		log.Fatalf("registering slash commands: %v", err)
	}
	log.Printf("registered %d slash commands (guild=%q)", len(registered), guildID)

	log.Println("leaguebot is running, press Ctrl+C to exit")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}
