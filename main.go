package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/mmcdole/gofeed"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
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
	log.Info().
		Str("function", "fetchRSS").
		Str("channel_id", channelID).
		Msg("Starting to fetch rss")
	feedParser := gofeed.NewParser()
	feedUrlList := feedStore.feedURLs[channelID]
	for _, feedUrl := range feedUrlList {
		feed, err := feedParser.ParseURL(feedUrl)
		if err != nil {
			log.Warn().
				Str("function", "parseURL").
				Str("channel_id", channelID).
				Str("feed_url", feedUrl).
				Msg("No feed found from this Url")
			continue
		}
		if len(feed.Items) == 0 {
			log.Warn().
				Str("function", "parseURL").
				Str("channel_id", channelID).
				Str("feed_url", feedUrl).
				Msg("No item found in the feed")
			continue
		}
		log.Info().
			Str("function", "fetchRSS").
			Str("channel_id", channelID).
			Msg("Successful fetching rss")
		return feed.Items[0], nil
	}
	return nil, fmt.Errorf("error: no items found for any feed")
}

func postToDiscord(session *discordgo.Session, channelID, msg string) {
	log.Info().
		Str("function", "postToDiscord").
		Str("channel_id", channelID).
		Str("msg", msg).
		Msg("Starting to post to discord channel")
	_, err := session.ChannelMessageSend(channelID, msg)
	if err != nil {
		log.Error().
			Err(err).
			Str("function", "postToDiscord").
			Str("channel_id", channelID).
			Str("msg", msg).
			Msg("Failed sending message to requested channel")
	}
	log.Info().
		Str("function", "postToDiscord").
		Str("channel_id", channelID).
		Str("msg", msg).
		Msg("Successful starting to post to discord channel")
}

func schedulePeriodicUpdates(session *discordgo.Session) {
	log.Info().Str("function", "schedulePeriodicUpdates").Msg("Starting scheduled job")
	c := cron.New()
	_, err := c.AddFunc(fmt.Sprintf("@every %s", feedStore.timeout.String()), func() {
		feedStore.mu.RLock()
		channels := make([]string, 0, len(feedStore.feedURLs))
		for channelID := range feedStore.feedURLs {
			channels = append(channels, channelID)
		}
		feedStore.mu.RUnlock()

		for _, channelID := range channels {
			feedItem, err := fetchRSS(channelID)
			if err != nil {
				log.Error().
					Err(err).
					Str("function", "fetchRss").
					Any("channel_id", channelID).
					Msg("Failed fetching rss feeds")
				continue
			}
			if feedItem != nil {
				message := fmt.Sprintf("🆕 New blog post: **%s**\n%s", feedItem.Title, feedItem.Link)
				postToDiscord(session, channelID, message)
			}
		}
	})
	if err != nil {
		log.Fatal().
			Err(err).
			Str("function", "c.Addfunc").
			Any("discord-session", session).
			Any("cron", c).
			Msg("Failed to schedule RSS job")
	}
	log.Info().Str("function", "schedulePeriodicUpdates").Msg("Successful starting scheduled job")
	c.Start()
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	log.Info().Str("function", "messageCreate").Msg("Started creating message")
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
		_, err := s.ChannelMessageSend(
			m.ChannelID,
			"Usage: /rss <add|remove|list|update_timeout> [url|duration]",
		)
		if err != nil {
			log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
		}
		return
	}
	command := args[1]
	switch command {
	case "add":
		if len(args) < 3 {
			_, err := s.ChannelMessageSend(m.ChannelID, "Please provide a feed URL to add")
			if err != nil {
				log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
			}
			return
		}
		if feedStore.FeedExists(m.ChannelID, args[2]) {
			_, err := s.ChannelMessageSend(m.ChannelID, "URL already exists.")
			if err != nil {
				log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
			}
			return
		}
		log.Info().
			Str("function", "AddFeed").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Started Adding feed")
		feedStore.AddFeed(m.ChannelID, args[2])
		_, err := s.ChannelMessageSend(m.ChannelID, "Feed added.")
		if err != nil {
			log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
		}
		log.Info().
			Str("function", "AddFeed").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Successful Adding feed")
		feedStore.AddFeed(m.ChannelID, args[2])

	case "remove":
		if len(args) < 3 {
			_, err := s.ChannelMessageSend(m.ChannelID, "Please provide a feed URL to remove")
			if err != nil {
				log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
			}
			return
		}
		if feedStore.FeedExists(m.ChannelID, args[2]) {
			_, err := s.ChannelMessageSend(m.ChannelID, "URL already exists.")
			if err != nil {
				log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
			}
			return
		}
		log.Info().
			Str("function", "RemoveFeed").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Started removing feed")
		feedStore.RemoveFeed(m.ChannelID, args[2])
		_, err := s.ChannelMessageSend(m.ChannelID, "Feed removed.")
		if err != nil {
			log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
		}
		log.Info().
			Str("function", "RemoveFeed").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Successful removing feed")

	case "list":
		log.Info().
			Str("function", "ListFeed").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Started listing feed.")
		response := feedStore.ListFeed(m.ChannelID)
		_, err := s.ChannelMessageSend(m.ChannelID, response)
		if err != nil {
			log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
		}
		log.Info().
			Str("function", "ListFeed").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Successful listing feed")

	case "update_timeout":
		if len(args) < 3 {
			_, err := s.ChannelMessageSend(m.ChannelID, "Usage: /rss update_timeout <10m|1h|etc>")
			if err != nil {
				log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
			}
			return
		}
		log.Info().
			Str("function", "UpdateTimeout").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Updating timeout")
		err := feedStore.UpdateTimeout(args[2])
		if err != nil {
			_, err := s.ChannelMessageSend(m.ChannelID, "Invalid timeout format: "+err.Error())
			if err != nil {
				log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
			}
			return
		}
		log.Info().
			Str("function", "UpdateTimeout").
			Str("channel_id", m.ChannelID).
			Str("feed_url", args[2]).
			Msg("Successful updating timeout")
		_, err = s.ChannelMessageSend(m.ChannelID, "Timeout updated to "+args[2])
		if err != nil {
			log.Error().Str("function", "messageCreate").Err(err).Msg("Failed sending message")
		}
	}
}

func runBot() (*discordgo.Session, error) {
	log.Info().Str("function", "runBot").Msg("Starting Bot")
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
	log.Info().
		Str("function", "runBot").
		Any("discord-session", session).
		Msg("Successful starting Bot")
	return session, nil
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal().Err(err).Str("function", "godotenv.Load").Msg("Failed loading .env")
	}
	session, err := runBot()
	if err != nil {
		log.Fatal().Err(err).Str("function", "runBot").Msg("Failed starting bot")
	}
	schedulePeriodicUpdates(session)
	select {}
}
