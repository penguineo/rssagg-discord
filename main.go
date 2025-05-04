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
