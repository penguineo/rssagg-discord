package main

import (
	"fmt"
	"log"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/mmcdole/gofeed"
)

func fetchRSS(channelID string) (*gofeed.Item, error) {
	feedParser := gofeed.NewParser()
	feedUrlList := feedStore.feedURLs[channelID]
	for _, feedUrl := range feedUrlList {
		feed, err := feedParser.ParseURL(feedUrl)
		if err != nil {
			continue
		}
		if len(feed.Items) == 0 {
			continue
		}
		return feed.Items[0], nil
	}
	return nil, fmt.Errorf("error: no items found for any feed")
}

func postToDiscord(session *discordgo.Session, channelID, message string) {
	_, err := session.ChannelMessageSend(channelID, message)
	if err != nil {
		log.Printf("Failed to send message to channel %s: %v\n", channelID, err)
	}
}

func runBot() (*discordgo.Session, error) {
	BotToken := os.Getenv("BOTAPI")
	session, err := discordgo.New("Bot " + BotToken)
	if err != nil {
		return nil, fmt.Errorf("error: creating new discord session: %w", err)
	}
	session.AddHandler(messageCreate)
	err = session.Open()
	if err != nil {
		return nil, fmt.Errorf("error: opening discord session: %w", err)
	}
	return session, nil
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error: Loading .env: ", err)
	}
	session, err := runBot()
	if err != nil {
		log.Fatal("error: Starting bot: ", err)
	}
	select {}
}
