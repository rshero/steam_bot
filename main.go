package main

import (
	"context"
	"log"
	"time"

	"steam_bot/bot"
	"steam_bot/config"
	"steam_bot/steam"
	"steam_bot/templates"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
)

func main() {
	cfg := config.LoadConfig()

	database, err := steam.InitDatabase(context.Background(), "steam.db")
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer database.Close()

	steam.SetDatabase(database)

	b, updater, dispatcher, err := bot.StartBot(cfg)
	if err != nil {
		log.Fatal("Failed to start bot:", err)
	}

	dispatcher.AddHandler(handlers.NewInlineQuery(nil, bot.HandleInlineQuery))
	dispatcher.AddHandler(handlers.NewCallback(nil, bot.NewCallbackQueryHandler(cfg)))

	// Command handlers
	cmdFilter, err := message.Regex(`^/(` + templates.CommandKeys() + `)(@` + b.User.Username + `)?(\s|$)`)
	if err != nil {
		log.Fatal("Failed to compile command regex:", err)
	}
	dispatcher.AddHandler(handlers.NewMessage(cmdFilter, bot.DynamicCmdHandler))

	// Game tracking commands
	dispatcher.AddHandler(handlers.NewCommand("addgame", func(b *gotgbot.Bot, ctx *ext.Context) error {
		return bot.HandleAddGameCommand(b, ctx, cfg)
	}))
	dispatcher.AddHandler(handlers.NewCommand("removegame", func(b *gotgbot.Bot, ctx *ext.Context) error {
		return bot.HandleRemoveGameCommand(b, ctx, cfg)
	}))
	dispatcher.AddHandler(handlers.NewCommand("editgame", func(b *gotgbot.Bot, ctx *ext.Context) error {
		return bot.HandleEditGameCommand(b, ctx, cfg)
	}))
	dispatcher.AddHandler(handlers.NewCommand("mygamestats", func(b *gotgbot.Bot, ctx *ext.Context) error {
		return bot.HandleMyGameStatsCommand(b, ctx, cfg)
	}))
	dispatcher.AddHandler(handlers.NewCommand("importsteam", func(b *gotgbot.Bot, ctx *ext.Context) error {
		return bot.HandleImportSteamCommand(b, ctx, cfg)
	}))

	// Handler for game edit replies (hours/notes)
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			return msg.ReplyToMessage != nil && msg.Text != ""
		},
		bot.HandleGameEditReply,
	))

	err = updater.StartPolling(b, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 600,
			},
		},
	})
	if err != nil {
		log.Fatal("Failed to start polling:", err)
	}
	log.Printf("%s has been started...\n", b.User.Username)

	go bot.SendDealsRoutine(b, cfg.ChannelID)

	updater.Idle()
}
