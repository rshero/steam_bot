package main

import (
	"log"
	"time"

	"steam_bot/bot"
	"steam_bot/config"
	"steam_bot/templates"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
)

func main() {
	cfg := config.LoadConfig()

	b, updater, dispatcher, err := bot.StartBot(cfg)
	if err != nil {
		log.Fatal("Failed to start bot:", err)
	}

	dispatcher.AddHandler(handlers.NewInlineQuery(nil, bot.HandleInlineQuery))
	dispatcher.AddHandler(handlers.NewCallback(nil, bot.NewCallbackQueryHandler(cfg)))

	cmdFilter, err := message.Regex(`^/(` + templates.CommandKeys() + `)(@` + b.User.Username + `)?(\s|$)`)
	if err != nil {
		log.Fatal("Failed to compile command regex:", err)
	}
	dispatcher.AddHandler(handlers.NewMessage(cmdFilter, bot.DynamicCmdHandler))

	err = updater.StartPolling(b, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 10,
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
