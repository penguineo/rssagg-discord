package main

import (
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/mmcdole/gofeed"
)

type FeedStore struct {
	mu       sync.RWMutex
	feedURLs map[string][]string
	timeout  time.Duration
}

var feedStore = FeedStore{
	feedURLs: make(map[string][]string),
	timeout:  30 * time.Minute,
}

func (fs *FeedStore) AddFeed(channelID, feedURL string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	feeds := fs.feedURLs[channelID]
	feeds = append(feeds, feedURL)
	fs.feedURLs[channelID] = feeds
}

func (fs *FeedStore) RemoveFeed(channelID, feedURL string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	feeds := fs.feedURLs[channelID]
	newFeeds := []string{}
	for _, stored := range feeds {
		if stored != feedURL {
			newFeeds = append(newFeeds, stored)
		}
	}
	fs.feedURLs[channelID] = newFeeds
}

func (fs *FeedStore) ListFeed(channelID string) string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	feeds := fs.feedURLs[channelID]
	if len(feeds) == 0 {
		return "No RSS feeds subscribed."
	}
	return "Subscribed feeds:\n" + strings.Join(feeds, "\n")
}

func (fs *FeedStore) UpdateTimeout(timeoutStr string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	d, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return err
	}
	fs.timeout = d
	return nil
}

func (fs *FeedStore) FeedExists(channelID, feedURL string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	feedURLs := feedStore.feedURLs[channelID]
	return slices.Contains(feedURLs, feedURL)
}

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


func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	if m.GuildID != "" {
		if !strings.HasPrefix(m.Content, "/rss") {
			return
		}
	}
	if !strings.HasPrefix(m.Content, "/rss") {
		return
	}
	args := strings.Fields(m.Content)
	if len(args) < 2 {
		s.ChannelMessageSend(
			m.ChannelID,
			"Usage: /rss <add|remove|list|update_timeout> [url|duration]",
		)
		return
	}
	command := args[1]
	switch command {
	case "add":
		if len(args) < 3 {
			s.ChannelMessageSend(m.ChannelID, "Please provide a feed URL to add")
			return
		}
		if feedStore.FeedExists(m.ChannelID, args[2]) {
			s.ChannelMessageSend(m.ChannelID, "URL already exists.")
			return
		}
		feedStore.AddFeed(m.ChannelID, args[2])
		s.ChannelMessageSend(m.ChannelID, "Feed added.")

	case "remove":
		if len(args) < 3 {
			s.ChannelMessageSend(m.ChannelID, "Please provide a feed URL to remove")
			return
		}
		if feedStore.FeedExists(m.ChannelID, args[2]) {
			s.ChannelMessageSend(m.ChannelID, "URL already exists.")
			return
		}
		feedStore.RemoveFeed(m.ChannelID, args[2])
		s.ChannelMessageSend(m.ChannelID, "Feed removed.")

	case "list":
		response := feedStore.ListFeed(m.ChannelID)
		s.ChannelMessageSend(m.ChannelID, response)

	case "update_timeout":
		if len(args) < 3 {
			s.ChannelMessageSend(m.ChannelID, "Usage: /rss update_timeout <10m|1h|etc>")
			return
		}
		err := feedStore.UpdateTimeout(args[2])
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "Invalid timeout format: "+err.Error())
			return
		}
		s.ChannelMessageSend(m.ChannelID, "Timeout updated to "+args[2])
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
