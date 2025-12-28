package bot

import (
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

// ----- Sent Posts Cache -----

var (
	sentPosts      = make(map[string]time.Time)
	sentPostsMu    sync.RWMutex
	maxCacheSize   = 200
	cleanupPercent = 0.5
)

// ----- Bot Initialization -----

func StartBot(cfg *config.Config) (*gotgbot.Bot, *ext.Updater, *ext.Dispatcher, error) {
	b, err := gotgbot.NewBot(cfg.BotToken, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating bot: %w", err)
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

// ----- Deals Routine -----

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

	if initializeDealsCache(deals) {
		return
	}

	for _, deal := range deals {
		if isAlreadySent(deal.DealID) {
			continue
		}

		if err := sendDeal(b, channelID, deal); err != nil {
			log.Println("Error sending deal:", err)
			continue
		}

		markAsSent(deal.DealID)
		time.Sleep(2 * time.Second)
	}
}

func initializeDealsCache(deals []steam.CheapSharkDeal) bool {
	sentPostsMu.Lock()
	defer sentPostsMu.Unlock()

	if len(sentPosts) > 0 {
		if len(sentPosts) > maxCacheSize {
			cleanupOldEntries()
		}
		return false
	}

	for _, deal := range deals {
		sentPosts[deal.DealID] = time.Now()
	}
	log.Printf("Initialized sent posts cache with %d items", len(deals))
	return true
}

func isAlreadySent(dealID string) bool {
	sentPostsMu.RLock()
	defer sentPostsMu.RUnlock()
	_, exists := sentPosts[dealID]
	return exists
}

func markAsSent(dealID string) {
	sentPostsMu.Lock()
	defer sentPostsMu.Unlock()
	sentPosts[dealID] = time.Now()
}

func sendDeal(b *gotgbot.Bot, channelID int64, deal steam.CheapSharkDeal) error {
	appInfo, err := steam.GetSteamAppInfo(deal.SteamAppID)
	if err != nil {
		return fmt.Errorf("getting details for app %s: %w", deal.SteamAppID, err)
	}

	msg := templates.FormatDealMessage(
		deal.Title,
		deal.NormalPrice,
		deal.SalePrice,
		appInfo.Price,
		deal.SteamRating,
		appInfo.Description,
		appInfo.HeaderImage,
		appInfo.Categories,
		appInfo.Genres,
	)

	_, err = b.SendMessage(channelID, msg, &gotgbot.SendMessageOpts{
		ParseMode: "HTML",
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
				{Text: "Claim Deal", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", deal.SteamAppID)},
			}},
		},
	})

	if err == nil {
		log.Println("Sent deal:", deal.Title)
	}
	return err
}

// cleanupOldEntries removes the oldest entries from sentPosts
// Must be called with sentPostsMu locked
func cleanupOldEntries() {
	if len(sentPosts) == 0 {
		return
	}

	type entry struct {
		id   string
		time time.Time
	}

	entries := make([]entry, 0, len(sentPosts))
	for id, t := range sentPosts {
		entries = append(entries, entry{id: id, time: t})
	}

	// Sort by timestamp (oldest first)
	for i := range len(entries) - 1 {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].time.After(entries[j].time) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	removeCount := int(float64(len(sentPosts)) * cleanupPercent)
	for _, e := range entries[:removeCount] {
		delete(sentPosts, e.id)
	}

	log.Printf("Cleaned up %d old entries from cache, %d remaining", removeCount, len(sentPosts))
}

// ----- Inline Query Handler -----

func handleInlineDotCommand(b *gotgbot.Bot, ctx *ext.Context, cmd string) error {
	userID := ctx.InlineQuery.From.Id

	// Handle ".mysteam" or ".mysteam username"
	if cmd == "mysteam" || strings.HasPrefix(cmd, "mysteam ") {
		return handleMySteamInlineQuery(b, ctx, cmd, userID)
	}

	inlineCmd, ok := templates.InlineCommands[cmd]
	if !ok {
		return nil
	}

	results := []gotgbot.InlineQueryResult{
		buildInlineCommandResult(cmd, inlineCmd),
	}

	_, err := ctx.InlineQuery.Answer(b, results, &gotgbot.AnswerInlineQueryOpts{
		CacheTime: 300,
	})
	return err
}

func handleMySteamInlineQuery(b *gotgbot.Bot, ctx *ext.Context, cmd string, userID int64) error {
	// Extract username after "mysteam "
	username, hasUsername := strings.CutPrefix(cmd, "mysteam ")
	username = strings.TrimSpace(username)

	inlineCmd := templates.InlineCommands["mysteam"]

	var result gotgbot.InlineQueryResultArticle

	if !hasUsername || username == "" {
		// No username provided - show help with switch inline button
		switchQuery := ".mysteam "
		result = gotgbot.InlineQueryResultArticle{
			Id:           "mysteam_help",
			Title:        inlineCmd.Title,
			Description:  inlineCmd.Description,
			ThumbnailUrl: inlineCmd.ThumbnailUrl,
			InputMessageContent: gotgbot.InputTextMessageContent{
				MessageText: inlineCmd.Message,
				ParseMode:   "HTML",
			},
			ReplyMarkup: &gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{{Text: "Enter username", SwitchInlineQueryCurrentChat: &switchQuery}},
				},
			},
		}
	} else {
		// Username provided - show result with callback button to fetch details
		result = gotgbot.InlineQueryResultArticle{
			Id:           "mysteam_" + username,
			Title:        fmt.Sprintf("Lookup: %s", username),
			Description:  "Click to fetch Steam profile",
			ThumbnailUrl: inlineCmd.ThumbnailUrl,
			InputMessageContent: gotgbot.InputTextMessageContent{
				MessageText: fmt.Sprintf("<b>Steam Profile: %s</b>\n\nClick the button below to fetch profile details.", username),
				ParseMode:   "HTML",
			},
			ReplyMarkup: &gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{{Text: "Fetch Profile", CallbackData: fmt.Sprintf("mysteam:%s_%d", username, userID)}},
				},
			},
		}
	}

	_, err := ctx.InlineQuery.Answer(b, []gotgbot.InlineQueryResult{result}, &gotgbot.AnswerInlineQueryOpts{
		CacheTime: 60,
	})
	return err
}

func showAllInlineCommands(b *gotgbot.Bot, ctx *ext.Context) error {
	results := make([]gotgbot.InlineQueryResult, 0, len(templates.InlineCommands))

	for name, cmd := range templates.InlineCommands {
		results = append(results, buildInlineCommandResult(name, cmd))
	}

	_, err := ctx.InlineQuery.Answer(b, results, &gotgbot.AnswerInlineQueryOpts{
		CacheTime: 300,
	})
	return err
}

func buildInlineCommandResult(name string, cmd templates.InlineCommand) gotgbot.InlineQueryResultArticle {
	result := gotgbot.InlineQueryResultArticle{
		Id:          "cmd_" + name,
		Title:       cmd.Title,
		Description: cmd.Description,
		InputMessageContent: gotgbot.InputTextMessageContent{
			MessageText: cmd.Message,
			ParseMode:   "HTML",
		},
		ThumbnailUrl: cmd.ThumbnailUrl,
	}

	// Build keyboard
	var buttons [][]gotgbot.InlineKeyboardButton

	// Add custom keyboard if defined
	if cmd.Keyboard != nil {
		for _, row := range cmd.Keyboard() {
			var btnRow []gotgbot.InlineKeyboardButton
			for _, btn := range row {
				b := gotgbot.InlineKeyboardButton{Text: btn.Text}
				if btn.URL != "" {
					b.Url = btn.URL
				}
				if btn.SwitchInlineQuery != "" {
					b.SwitchInlineQueryCurrentChat = &btn.SwitchInlineQuery
				}
				btnRow = append(btnRow, b)
			}
			buttons = append(buttons, btnRow)
		}
	}

	// Add "Try" button if SwitchQuery is set
	if cmd.SwitchQuery != "" {
		switchQuery := cmd.SwitchQuery
		buttons = append(buttons, []gotgbot.InlineKeyboardButton{
			{Text: "Try it", SwitchInlineQueryCurrentChat: &switchQuery},
		})
	}

	if len(buttons) > 0 {
		result.ReplyMarkup = &gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: buttons,
		}
	}

	return result
}

func HandleInlineQuery(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.InlineQuery.Query

	// Show all commands menu when query is empty
	if query == "" {
		return showAllInlineCommands(b, ctx)
	}

	// Handle dot commands (e.g., ".help")
	if cmd, ok := strings.CutPrefix(query, "."); ok {
		return handleInlineDotCommand(b, ctx, cmd)
	}

	userID := ctx.InlineQuery.From.Id
	results, err := steam.SearchSteam(query)
	if err != nil {
		log.Println("Error searching steam:", err)
		return nil
	}

	inlineResults := processSearchResults(results, userID)

	_, err = ctx.InlineQuery.Answer(b, inlineResults, &gotgbot.AnswerInlineQueryOpts{
		CacheTime: 100,
	})
	return err
}

func processSearchResults(results []steam.SteamSearchItem, userID int64) []gotgbot.InlineQueryResult {
	inlineResults := make([]gotgbot.InlineQueryResult, len(results))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 3) // Limit concurrent API calls

	for idx, item := range results {
		wg.Add(1)
		go func(i int, item steam.SteamSearchItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			inlineResults[i] = buildInlineResult(i, item, userID)
		}(idx, item)
	}

	wg.Wait()
	return inlineResults
}

func buildInlineResult(index int, item steam.SteamSearchItem, userID int64) gotgbot.InlineQueryResultArticle {
	appID := strconv.Itoa(item.ID)
	appInfo, _ := steam.GetSteamAppInfo(appID) // Uses cache from GetFullSteamAppDetails

	usPrice := float64(item.Price.Final) / 100.0
	usPriceStr := fmt.Sprintf("$%.2f", usPrice)

	priceDisplay := formatPriceDisplay(usPrice, appInfo.Price)
	imageURL := firstNonEmpty(appInfo.HeaderImage, item.TinyImage)

	msg := templates.FormatDealMessage(
		item.Name,
		usPriceStr,
		"",
		appInfo.Price,
		"",
		appInfo.Description,
		imageURL,
		appInfo.Categories,
		appInfo.Genres,
	)

	return gotgbot.InlineQueryResultArticle{
		Id:           strconv.Itoa(index),
		Title:        item.Name,
		Description:  fmt.Sprintf("Price: %s", priceDisplay),
		ThumbnailUrl: item.TinyImage,
		InputMessageContent: gotgbot.InputTextMessageContent{
			MessageText: msg,
			ParseMode:   "HTML",
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: false,
			},
		},
		ReplyMarkup: buildInlineKeyboard(item.ID, userID),
	}
}

func formatPriceDisplay(usPrice float64, inrPrice string) string {
	if usPrice == 0 {
		return inrPrice
	}
	var priceStr string
	if inrPrice != "" {
		priceStr = fmt.Sprintf("$%.2f / %s", usPrice, inrPrice)
	} else {
		priceStr = fmt.Sprintf("$%.2f", usPrice)
	}
	return priceStr
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func buildInlineKeyboard(appID int, userID int64) *gotgbot.InlineKeyboardMarkup {
	return &gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%d", appID)},
				{Text: "SteamDB", Url: fmt.Sprintf("https://steamdb.info/app/%d/", appID)},
			},
			{
				{Text: "Details", CallbackData: fmt.Sprintf("details:%d_%d", appID, userID)},
				{Text: "Requirements", CallbackData: fmt.Sprintf("requirements:%d_%d", appID, userID)},
			},
		},
	}
}

// ----- Callback Query Handler -----

// CallbackType represents the type of callback query
type CallbackType int

const (
	CallbackUnknown CallbackType = iota
	CallbackDetails
	CallbackRequirements
	CallbackHLTB
	CallbackMySteam
	CallbackBack
)

// CallbackData holds parsed callback information
type CallbackData struct {
	Type   CallbackType
	AppID  string // Also used as username for mysteam
	UserID int64
}

// NewCallbackQueryHandler creates a callback query handler with config access
func NewCallbackQueryHandler(cfg *config.Config) func(b *gotgbot.Bot, ctx *ext.Context) error {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		return HandleCallbackQuery(b, ctx, cfg)
	}
}

func HandleCallbackQuery(b *gotgbot.Bot, ctx *ext.Context, cfg *config.Config) error {
	cbData, err := parseCallbackData(ctx.CallbackQuery.Data)
	if err != nil || cbData.Type == CallbackUnknown {
		return nil
	}

	// Verify user authorization
	if cbData.UserID != ctx.CallbackQuery.From.Id {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "This is not for you",
			ShowAlert: true,
		})
		return nil
	}

	// Handle mysteam callback separately (doesn't need app details)
	if cbData.Type == CallbackMySteam {
		return handleMySteamCallback(b, ctx, cbData, cfg)
	}

	// Handle back callback (uses cache to restore original view)
	if cbData.Type == CallbackBack {
		return handleBackCallback(b, ctx, cbData)
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Fetching..."})

	// Fetch app details once (cached)
	details, err := steam.GetFullSteamAppDetails(cbData.AppID)
	if err != nil {
		log.Println("Error getting details:", err)
		return nil
	}

	// Route to appropriate handler
	msg, replyMarkup := routeCallback(cbData, details)
	if msg == "" {
		return nil
	}

	return sendCallbackResponse(b, ctx, msg, replyMarkup)
}

func handleBackCallback(b *gotgbot.Bot, ctx *ext.Context, cbData CallbackData) error {
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Going back..."})

	// Try to get from cache first
	cache := steam.GetAppDetailsCache()
	details, cached := cache.Get(cbData.AppID)

	// If not cached, fetch it
	if !cached {
		var err error
		details, err = steam.GetFullSteamAppDetails(cbData.AppID)
		if err != nil {
			log.Println("Error getting details for back navigation:", err)
			return nil
		}
	}

	// Get app info for pricing
	appInfo, _ := steam.GetSteamAppInfo(cbData.AppID)

	// Parse appID to int for URL generation
	appIDInt, _ := strconv.Atoi(cbData.AppID)

	// Format price display
	var priceDisplay string
	if appInfo.Price != "" {
		priceDisplay = appInfo.Price
	} else {
		priceDisplay = "Free"
	}

	// Reconstruct the original search result message
	msg := templates.FormatDealMessage(
		details.Name,
		priceDisplay,
		"",
		appInfo.Price,
		"",
		appInfo.Description,
		appInfo.HeaderImage,
		appInfo.Categories,
		appInfo.Genres,
	)

	// Build the original inline keyboard
	replyMarkup := buildInlineKeyboard(appIDInt, cbData.UserID)

	return sendCallbackResponse(b, ctx, msg, *replyMarkup)
}

func handleMySteamCallback(b *gotgbot.Bot, ctx *ext.Context, cbData CallbackData, cfg *config.Config) error {
	username := cbData.AppID // AppID field holds the username for mysteam

	// Check if username is empty
	if username == "" {
		switchQuery := ".mysteam "
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Please provide a username. Try: .mysteam username",
			ShowAlert: true,
		})
		// Update button to prompt for username
		_, _, _ = b.EditMessageReplyMarkup(&gotgbot.EditMessageReplyMarkupOpts{
			InlineMessageId: ctx.CallbackQuery.InlineMessageId,
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{{Text: "Enter username", SwitchInlineQueryCurrentChat: &switchQuery}},
				},
			},
		})
		return nil
	}

	// Check if API key is configured
	if cfg.SteamAPIKey == "" {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Steam API key not configured",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Fetching profile..."})

	// Fetch user info
	userInfo, err := steam.GetSteamUserInfo(cfg.SteamAPIKey, username)
	if err != nil {
		log.Println("Error getting Steam user info:", err)
		_, _, _ = b.EditMessageText(fmt.Sprintf("<b>Error:</b> User not found: %s", username), &gotgbot.EditMessageTextOpts{
			InlineMessageId: ctx.CallbackQuery.InlineMessageId,
			ParseMode:       "HTML",
		})
		return nil
	}

	// Format and send the profile
	msg := templates.FormatSteamUserProfile(
		userInfo.Summary.PersonaName,
		userInfo.Summary.ProfileURL,
		userInfo.Summary.Avatar,
		userInfo.Summary.PersonaState,
		userInfo.Level,
		userInfo.GameCount,
		userInfo.Summary.CountryCode,
	)

	replyMarkup := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "View Profile", Url: userInfo.Summary.ProfileURL}},
		},
	}

	_, _, err = b.EditMessageText(msg, &gotgbot.EditMessageTextOpts{
		InlineMessageId: ctx.CallbackQuery.InlineMessageId,
		ParseMode:       "HTML",
		ReplyMarkup:     replyMarkup,
	})
	return err
}

func parseCallbackData(data string) (CallbackData, error) {
	result := CallbackData{}

	prefixes := map[string]CallbackType{
		"details:":      CallbackDetails,
		"more_details:": CallbackDetails, // Support legacy format
		"requirements:": CallbackRequirements,
		"hltb:":         CallbackHLTB,
		"mysteam:":      CallbackMySteam,
		"back:":         CallbackBack,
	}

	var payload string
	for prefix, cbType := range prefixes {
		if strings.HasPrefix(data, prefix) {
			result.Type = cbType
			payload = strings.TrimPrefix(data, prefix)
			break
		}
	}

	if result.Type == CallbackUnknown {
		return result, nil
	}

	parts := strings.Split(payload, "_")
	if len(parts) != 2 {
		return result, fmt.Errorf("invalid callback format")
	}

	result.AppID = parts[0]

	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return result, fmt.Errorf("invalid user ID: %w", err)
	}
	result.UserID = userID

	return result, nil
}

func routeCallback(cbData CallbackData, details *steam.SteamAppDetails) (string, gotgbot.InlineKeyboardMarkup) {
	switch cbData.Type {
	case CallbackDetails:
		return handleDetailsCallback(cbData, details)
	case CallbackRequirements:
		return handleRequirementsCallback(cbData, details)
	case CallbackHLTB:
		return handleHLTBCallback(cbData, details)
	default:
		return "", gotgbot.InlineKeyboardMarkup{}
	}
}

func handleDetailsCallback(cbData CallbackData, details *steam.SteamAppDetails) (string, gotgbot.InlineKeyboardMarkup) {
	reviews := fetchReviews(cbData.AppID)

	msg := templates.FormatMoreDetails(
		details.Name,
		details.CategoryNames(),
		details.GenreNames(),
		details.Metacritic.Score,
		details.Metacritic.URL,
		reviews.ReviewScoreDesc,
		reviews.TotalPositive,
		reviews.TotalNegative,
		reviews.TotalReviews,
		0, 0, 0, // HLTB values - user can fetch via button
		details.Developers,
		details.Publishers,
		nil,
		details.ReleaseDate.Date,
	)

	replyMarkup := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", cbData.AppID)},
			},
			{
				{Text: "Requirements", CallbackData: fmt.Sprintf("requirements:%s_%d", cbData.AppID, cbData.UserID)},
				{Text: "⏱️ HLTB", CallbackData: fmt.Sprintf("hltb:%s_%d", cbData.AppID, cbData.UserID)},
			},
			{
				{Text: "❮", CallbackData: fmt.Sprintf("back:%s_%d", cbData.AppID, cbData.UserID)},
			},
		},
	}

	return msg, replyMarkup
}

func handleRequirementsCallback(cbData CallbackData, details *steam.SteamAppDetails) (string, gotgbot.InlineKeyboardMarkup) {
	reqs := details.GetPcRequirements()
	msg := templates.FormatRequirementsMessage(details.Name, reqs.Minimum, reqs.Recommended)

	replyMarkup := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", cbData.AppID)},
			},
			{
				{Text: "Details", CallbackData: fmt.Sprintf("details:%s_%d", cbData.AppID, cbData.UserID)},
			},
			{
				{Text: "❮", CallbackData: fmt.Sprintf("back:%s_%d", cbData.AppID, cbData.UserID)},
			},
		},
	}

	return msg, replyMarkup
}

func handleHLTBCallback(cbData CallbackData, details *steam.SteamAppDetails) (string, gotgbot.InlineKeyboardMarkup) {
	reviews := fetchReviews(cbData.AppID)

	hltbResult, err := steam.GetHltbData(details.Name)
	if err != nil {
		log.Println("Error getting HLTB data:", err)
		return "", gotgbot.InlineKeyboardMarkup{}
	}

	msg := templates.FormatMoreDetails(
		details.Name,
		details.CategoryNames(),
		details.GenreNames(),
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

	replyMarkup := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", cbData.AppID)},
				{Text: "Requirements", CallbackData: fmt.Sprintf("requirements:%s_%d", cbData.AppID, cbData.UserID)},
			},
			{
				{Text: "❮", CallbackData: fmt.Sprintf("back:%s_%d", cbData.AppID, cbData.UserID)},
			},
		},
	}

	return msg, replyMarkup
}

func fetchReviews(appID string) *steam.SteamReviewSummary {
	reviews, err := steam.GetSteamAppReviews(appID)
	if err != nil {
		log.Println("Error getting reviews:", err)
		return &steam.SteamReviewSummary{}
	}
	return reviews
}

func sendCallbackResponse(b *gotgbot.Bot, ctx *ext.Context, msg string, replyMarkup gotgbot.InlineKeyboardMarkup) error {
	if ctx.CallbackQuery.InlineMessageId != "" {
		_, _, err := b.EditMessageText(msg, &gotgbot.EditMessageTextOpts{
			InlineMessageId: ctx.CallbackQuery.InlineMessageId,
			ParseMode:       "HTML",
			ReplyMarkup:     replyMarkup,
		})
		return err
	}

	if ctx.CallbackQuery.Message != nil {
		_, _, err := ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: replyMarkup,
		})
		return err
	}

	return nil
}

func DynamicCmdHandler(b *gotgbot.Bot, ctx *ext.Context) error {
	text := ctx.EffectiveMessage.Text
	// Extract command: remove leading /, split by @ or space, take first part
	cmd := strings.TrimPrefix(text, "/")
	if idx := strings.IndexAny(cmd, "@ \t"); idx != -1 {
		cmd = cmd[:idx]
	}

	reply, ok := templates.Commands[cmd]
	if !ok {
		return nil
	}

	_, err := ctx.EffectiveMessage.Reply(b, reply, &gotgbot.SendMessageOpts{
		ParseMode: "HTML",
	})
	return err
}
