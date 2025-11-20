package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken  string
	ChannelID int64
}

func LoadConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, relying on environment variables")
	}

	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		log.Fatal("BOT_TOKEN is not set")
	}

	channelIDStr := os.Getenv("CHANNEL_ID")
	if channelIDStr == "" {
		log.Fatal("CHANNEL_ID is not set")
	}

	channelID, err := strconv.ParseInt(channelIDStr, 10, 64)
	if err != nil {
		log.Fatalf("Invalid CHANNEL_ID: %v", err)
	}

	return &Config{
		BotToken:  botToken,
		ChannelID: channelID,
	}
}
