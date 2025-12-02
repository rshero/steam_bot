package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"steam_bot/config"
	"steam_bot/steam"
	"steam_bot/templates"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

var (
	sentPosts      = make(map[string]time.Time)
	sentPostsMu    sync.RWMutex
	maxCacheSize   = 200
	cleanupPercent = 0.5 // Remove oldest 50% when limit reached
)

func StartBot(cfg *config.Config) (*gotgbot.Bot, *ext.Updater, *ext.Dispatcher, error) {
	b, err := gotgbot.NewBot(cfg.BotToken, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Println("Error handling update:", err.Error())
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	updater := ext.NewUpdater(dispatcher, nil)

	return b, updater, dispatcher, nil
}

func SendDealsRoutine(b *gotgbot.Bot, channelID int64) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	checkAndSendDeals(b, channelID)

	for range ticker.C {
		checkAndSendDeals(b, channelID)
	}
}

func checkAndSendDeals(b *gotgbot.Bot, channelID int64) {
	log.Println("Checking for deals...")
	deals, err := steam.GetCheapSharkDeals()
	if err != nil {
		log.Println("Error fetching deals:", err)
		return
	}

	sentPostsMu.Lock()
	if len(sentPosts) == 0 {
		for _, deal := range deals {
			sentPosts[deal.DealID] = time.Now()
		}
		sentPostsMu.Unlock()
		log.Println("Initialized sent posts cache with", len(deals), "items")
		return
	}

	// Clean up oldest entries if we exceed the limit
	if len(sentPosts) > maxCacheSize {
		cleanupOldEntries()
	}
	sentPostsMu.Unlock()

	for _, deal := range deals {
		sentPostsMu.RLock()
		_, alreadySent := sentPosts[deal.DealID]
		sentPostsMu.RUnlock()

		if alreadySent {
			continue
		}

		desc, imgURL, inrPrice, categories, genres, err := steam.GetSteamAppDetails(deal.SteamAppID)
		if err != nil {
			log.Printf("Error getting details for app %s: %v", deal.SteamAppID, err)
			continue
		}

		msg := templates.FormatDealMessage(deal.Title, deal.NormalPrice, deal.SalePrice, inrPrice, deal.SteamRating, desc, imgURL, categories, genres)

		_, err = b.SendMessage(channelID, msg, &gotgbot.SendMessageOpts{
			ParseMode: "HTML",
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
					{Text: "Claim Deal", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", deal.SteamAppID)},
				}},
			},
		})
		if err != nil {
			log.Println("Error sending message:", err)
		} else {
			log.Println("Sent deal:", deal.Title)
			sentPostsMu.Lock()
			sentPosts[deal.DealID] = time.Now()
			sentPostsMu.Unlock()
		}

		time.Sleep(2 * time.Second)
	}
}

// cleanupOldEntries removes the oldest entries from sentPosts
// Must be called with sentPostsMu locked
func cleanupOldEntries() {
	if len(sentPosts) == 0 {
		return
	}

	// Create a slice of entries sorted by timestamp
	type entry struct {
		id   string
		time time.Time
	}
	entries := make([]entry, 0, len(sentPosts))
	for id, t := range sentPosts {
		entries = append(entries, entry{id: id, time: t})
	}

	// Sort by timestamp (oldest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].time.After(entries[j].time) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove oldest entries
	removeCount := int(float64(len(sentPosts)) * cleanupPercent)
	for _, e := range entries[:removeCount] {
		delete(sentPosts, e.id)
	}

	log.Printf("Cleaned up %d old entries from cache, %d remaining", removeCount, len(sentPosts))
}

func HandleInlineQuery(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.InlineQuery.Query
	if query == "" {
		return nil
	}
	user_id := ctx.InlineQuery.From.Id
	results, err := steam.SearchSteam(query)
	if err != nil {
		log.Println("Error searching steam:", err)
		return nil
	}

	inlineResults := make([]gotgbot.InlineQueryResult, len(results))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)

	for idx, item := range results {
		wg.Add(1)
		go func(i int, item steam.SteamSearchItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			price := float64(item.Price.Final) / 100.0
			priceStr := fmt.Sprintf("%.2f", price)

			desc, headerImage, inrPrice, categories, genres, _ := steam.GetSteamAppDetails(strconv.Itoa(item.ID))
			if inrPrice == "" {
				inrPrice = "N/A"
			}

			imgToUse := headerImage
			if imgToUse == "" {
				imgToUse = item.TinyImage
			}

			msg := templates.FormatDealMessage(item.Name, priceStr, "", inrPrice, "", desc, imgToUse, categories, genres)

			inlineResults[i] = gotgbot.InlineQueryResultArticle{
				Id:           strconv.Itoa(i),
				Title:        item.Name,
				Description:  fmt.Sprintf("Price: $%s", priceStr),
				ThumbnailUrl: item.TinyImage,
				InputMessageContent: gotgbot.InputTextMessageContent{
					MessageText: msg,
					ParseMode:   "HTML",
					LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
						IsDisabled: false,
					},
				},
				ReplyMarkup: &gotgbot.InlineKeyboardMarkup{
					InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
						{
							{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%d", item.ID)},
							{Text: "SteamDB", Url: fmt.Sprintf("https://steamdb.info/app/%d/", item.ID)},
						},
						{
							{Text: "Details", CallbackData: fmt.Sprintf("more_details:%d_%d", item.ID, user_id)},
							{Text: "Requirements", CallbackData: fmt.Sprintf("requirements:%d_%d", item.ID, user_id)},
						},
					},
				},
			}
		}(idx, item)
	}

	wg.Wait()

	_, err = ctx.InlineQuery.Answer(b, inlineResults, &gotgbot.AnswerInlineQueryOpts{
		CacheTime: 100,
	})
	return err
}

func HandleCallbackQuery(b *gotgbot.Bot, ctx *ext.Context) error {
	data := ctx.CallbackQuery.Data

	isDetailsQuery := strings.HasPrefix(data, "more_details:")
	isRequirementsQuery := strings.HasPrefix(data, "requirements:")
	isHltbQuery := strings.HasPrefix(data, "hltb:")

	if !isDetailsQuery && !isRequirementsQuery && !isHltbQuery {
		return nil
	}

	var cbData, appID string
	var userID int64
	var err error

	if isDetailsQuery {
		cbData = strings.TrimPrefix(data, "more_details:")
	} else if isRequirementsQuery {
		cbData = strings.TrimPrefix(data, "requirements:")
	} else {
		cbData = strings.TrimPrefix(data, "hltb:")
	}

	appID = strings.Split(cbData, "_")[0]
	userID, err = strconv.ParseInt(strings.Split(cbData, "_")[1], 10, 64)
	if err != nil {
		return err
	}

	if userID != ctx.CallbackQuery.From.Id {
		ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "This is not for you", ShowAlert: true})
		return nil
	}

	ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Fetching..."})

	details, err := steam.GetFullSteamAppDetails(appID)
	if err != nil {
		log.Println("Error getting details:", err)
		return nil
	}

	var msg string
	var replyMarkup gotgbot.InlineKeyboardMarkup

	if isRequirementsQuery {
		// Handle Requirements callback
		var reqs steam.PcRequirements
		_ = json.Unmarshal(details.PcRequirements, &reqs)

		msg = templates.FormatRequirementsMessage(details.Name, reqs.Minimum, reqs.Recommended)
		replyMarkup = gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", appID)},
				},
				{
					{Text: "Details", CallbackData: fmt.Sprintf("more_details:%s_%d", appID, userID)},
				},
			},
		}
	} else if isDetailsQuery {
		// Handle Details callback
		reviews, err := steam.GetSteamAppReviews(appID)
		if err != nil {
			log.Println("Error getting reviews:", err)
			reviews = &steam.SteamReviewSummary{}
		}

		// Extract category descriptions
		categories := make([]string, 0, len(details.Categories))
		for _, cat := range details.Categories {
			categories = append(categories, cat.Description)
		}

		// Extract genre descriptions
		genres := make([]string, 0, len(details.Genres))
		for _, genre := range details.Genres {
			genres = append(genres, genre.Description)
		}

		// Pass 0 for HLTB values and empty platforms - user can fetch via HLTB button
		msg = templates.FormatMoreDetails(
			details.Name,
			categories,
			genres,
			details.Metacritic.Score,
			details.Metacritic.URL,
			reviews.ReviewScoreDesc,
			reviews.TotalPositive,
			reviews.TotalNegative,
			reviews.TotalReviews,
			0, 0, 0, // HLTB: MainStory, MainPlusExtra, Completionist
			details.Developers,
			details.Publishers,
			nil, // Empty platforms
			details.ReleaseDate.Date,
		)
		replyMarkup = gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", appID)},
				},
				{
					{Text: "Requirements", CallbackData: fmt.Sprintf("requirements:%s_%d", appID, userID)},
					{Text: "⏱️ HLTB", CallbackData: fmt.Sprintf("hltb:%s_%d", appID, userID)},
				},
			},
		}
	} else {
		// Handle HLTB callback
		reviews, err := steam.GetSteamAppReviews(appID)
		if err != nil {
			log.Println("Error getting reviews:", err)
			reviews = &steam.SteamReviewSummary{}
		}

		hltbResult, err := steam.GetHltbData(details.Name)
		if err != nil {
			log.Println("Error getting hltb data:", err)
			ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Failed to fetch HLTB data", ShowAlert: true})
			return nil
		}

		// Extract category descriptions
		categories := make([]string, 0, len(details.Categories))
		for _, cat := range details.Categories {
			categories = append(categories, cat.Description)
		}

		// Extract genre descriptions
		genres := make([]string, 0, len(details.Genres))
		for _, genre := range details.Genres {
			genres = append(genres, genre.Description)
		}

		msg = templates.FormatMoreDetails(
			details.Name,
			categories,
			genres,
			details.Metacritic.Score,
			details.Metacritic.URL,
			reviews.ReviewScoreDesc,
			reviews.TotalPositive,
			reviews.TotalNegative,
			reviews.TotalReviews,
			hltbResult.MainStory,
			hltbResult.MainPlusExtra,
			hltbResult.Completionist,
			details.Developers,
			details.Publishers,
			hltbResult.Platforms,
			details.ReleaseDate.Date,
		)
		replyMarkup = gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", appID)},
					{Text: "Requirements", CallbackData: fmt.Sprintf("requirements:%s_%d", appID, userID)},
				},
			},
		}
	}

	if ctx.CallbackQuery.InlineMessageId != "" {
		_, _, err = b.EditMessageText(msg, &gotgbot.EditMessageTextOpts{
			InlineMessageId: ctx.CallbackQuery.InlineMessageId,
			ParseMode:       "HTML",
			ReplyMarkup:     replyMarkup,
		})
		return err
	}

	if ctx.CallbackQuery.Message != nil {
		_, _, err = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: replyMarkup,
		})
		return err
	}

	return nil
}
