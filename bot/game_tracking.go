package bot

import (
	"context"
	"database/sql"
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

// extractCommandArgs extracts arguments from a command, handling @botusername suffix
// e.g., "/addgame@botname game name" -> "game name"
func extractCommandArgs(text, command string) string {
	// Remove the command prefix (e.g., "/addgame")
	args := strings.TrimPrefix(text, "/"+command)
	// Remove @botusername if present (everything up to the first space)
	if strings.HasPrefix(args, "@") {
		if idx := strings.Index(args, " "); idx != -1 {
			args = args[idx:]
		} else {
			args = ""
		}
	}
	return strings.TrimSpace(args)
}

// editMessageText is a helper that handles editing both regular and inline messages
func editMessageText(b *gotgbot.Bot, ctx *ext.Context, text string, opts *gotgbot.EditMessageTextOpts) error {
	if ctx.CallbackQuery.InlineMessageId != "" {
		// Inline message - use InlineMessageId
		if opts == nil {
			opts = &gotgbot.EditMessageTextOpts{}
		}
		opts.InlineMessageId = ctx.CallbackQuery.InlineMessageId
		_, _, err := b.EditMessageText(text, opts)
		// Ignore "message is not modified" error
		if err != nil && strings.Contains(err.Error(), "message is not modified") {
			return nil
		}
		return err
	}
	// Regular message
	if ctx.CallbackQuery.Message != nil {
		_, _, err := ctx.CallbackQuery.Message.EditText(b, text, opts)
		// Ignore "message is not modified" error
		if err != nil && strings.Contains(err.Error(), "message is not modified") {
			return nil
		}
		return err
	}
	return nil
}

// PendingEditType represents the type of pending edit
type PendingEditType int

const (
	PendingEditHours PendingEditType = iota
	PendingEditNotes
)

// PendingEdit holds info about a pending edit waiting for user reply
type PendingEdit struct {
	Type      PendingEditType
	GameID    int64
	UserID    int64
	GameName  string
	ChatID    int64
	MessageID int64
	CreatedAt time.Time
}

var (
	pendingEdits   = make(map[int64]*PendingEdit) // key: message ID
	pendingEditsMu sync.RWMutex
)

// AddGameFlags holds optional flags for the addgame command
type AddGameFlags struct {
	Status   *steam.GameStatus
	Playtime *float64
}

var (
	addGameFlagsStore = make(map[int64]*AddGameFlags) // key: user ID
	addGameFlagsMu    sync.RWMutex
)

// storeAddGameFlags stores flags for a user's addgame command
func storeAddGameFlags(userID int64, flags *AddGameFlags) {
	addGameFlagsMu.Lock()
	defer addGameFlagsMu.Unlock()
	addGameFlagsStore[userID] = flags
}

// getAndClearAddGameFlags retrieves and removes flags for a user
func getAndClearAddGameFlags(userID int64) *AddGameFlags {
	addGameFlagsMu.Lock()
	defer addGameFlagsMu.Unlock()
	flags, ok := addGameFlagsStore[userID]
	if ok {
		delete(addGameFlagsStore, userID)
	}
	return flags
}

// StorePendingEdit stores a pending edit operation
func StorePendingEdit(messageID int64, edit *PendingEdit) {
	pendingEditsMu.Lock()
	defer pendingEditsMu.Unlock()
	pendingEdits[messageID] = edit
}

// GetPendingEdit retrieves and removes a pending edit
func GetPendingEdit(messageID int64) *PendingEdit {
	pendingEditsMu.Lock()
	defer pendingEditsMu.Unlock()
	edit, ok := pendingEdits[messageID]
	if ok {
		delete(pendingEdits, messageID)
	}
	return edit
}

// CleanupOldPendingEdits removes pending edits older than 10 minutes
func CleanupOldPendingEdits() {
	pendingEditsMu.Lock()
	defer pendingEditsMu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	for msgID, edit := range pendingEdits {
		if edit.CreatedAt.Before(cutoff) {
			delete(pendingEdits, msgID)
		}
	}
}

// GameTrackingCallbackType represents game tracking callback types
type GameTrackingCallbackType int

const (
	GTCallbackUnknown GameTrackingCallbackType = iota
	GTCallbackAddGame
	GTCallbackAddCustomGame
	GTCallbackRemoveGame
	GTCallbackConfirmRemove
	GTCallbackCancelRemove
	GTCallbackEditGame
	GTCallbackEditStatus
	GTCallbackEditHours
	GTCallbackEditFavorite
	GTCallbackEditRating
	GTCallbackEditNotes
	GTCallbackListGames
	GTCallbackListFavorites
	GTCallbackListCompleted
	GTCallbackListBacklog
	GTCallbackListPlaying
	GTCallbackStatsRefresh
	GTCallbackStatsBack
	GTCallbackInlineStats
	GTCallbackStatusSet
	GTCallbackRatingSet
	GTCallbackImportAll
	GTCallbackImportPlayed
	GTCallbackBackToEditList
	GTCallbackBackToEditGameList
	GTCallbackEditGameList
)

// GameTrackingCallbackData holds parsed callback info for game tracking
type GameTrackingCallbackData struct {
	Type   GameTrackingCallbackType
	AppID  string
	GameID int64
	UserID int64
	Page   int
	Extra  string // For status values, etc.
}

// NewGameTrackingHandler creates the main callback router for game tracking
func NewGameTrackingHandler(cfg *config.Config) func(b *gotgbot.Bot, ctx *ext.Context) error {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		return HandleGameTrackingCallback(b, ctx, cfg)
	}
}

func HandleGameTrackingCallback(b *gotgbot.Bot, ctx *ext.Context, cfg *config.Config) error {
	cbData, err := parseGameTrackingCallback(ctx.CallbackQuery.Data)
	if err != nil || cbData.Type == GTCallbackUnknown {
		return nil
	}

	// Verify user authorization
	if cbData.UserID != 0 && cbData.UserID != ctx.CallbackQuery.From.Id {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "This is not for you",
			ShowAlert: true,
		})
		return nil
	}

	userID := ctx.CallbackQuery.From.Id
	cbData.UserID = userID

	// Route to appropriate handler
	switch cbData.Type {
	case GTCallbackAddGame:
		return handleAddGameCallback(b, ctx, cbData, cfg)
	case GTCallbackAddCustomGame:
		return handleAddCustomGameCallback(b, ctx, cbData, cfg)
	case GTCallbackRemoveGame:
		return handleRemoveGameCallback(b, ctx, cbData)
	case GTCallbackConfirmRemove:
		return handleConfirmRemoveCallback(b, ctx, cbData)
	case GTCallbackCancelRemove:
		return handleCancelRemoveCallback(b, ctx, cbData)
	case GTCallbackEditGame:
		return handleEditGameCallback(b, ctx, cbData)
	case GTCallbackEditStatus:
		return handleEditStatusCallback(b, ctx, cbData)
	case GTCallbackEditFavorite:
		return handleEditFavoriteCallback(b, ctx, cbData)
	case GTCallbackEditHours:
		return handleEditHoursCallback(b, ctx, cbData)
	case GTCallbackEditRating:
		return handleEditRatingCallback(b, ctx, cbData)
	case GTCallbackEditNotes:
		return handleEditNotesCallback(b, ctx, cbData)
	case GTCallbackStatusSet:
		return handleStatusSetCallback(b, ctx, cbData)
	case GTCallbackRatingSet:
		return handleRatingSetCallback(b, ctx, cbData)
	case GTCallbackImportAll:
		return handleImportAllCallback(b, ctx, cbData, cfg)
	case GTCallbackImportPlayed:
		return handleImportPlayedCallback(b, ctx, cbData, cfg)
	case GTCallbackListGames:
		return handleListGamesCallback(b, ctx, cbData, nil, false)
	case GTCallbackListFavorites:
		return handleListGamesCallback(b, ctx, cbData, nil, true)
	case GTCallbackListCompleted:
		status := steam.StatusCompleted
		return handleListGamesCallback(b, ctx, cbData, &status, false)
	case GTCallbackListBacklog:
		return handleListBacklogCallback(b, ctx, cbData)
	case GTCallbackListPlaying:
		status := steam.StatusPlaying
		return handleListGamesCallback(b, ctx, cbData, &status, false)
	case GTCallbackStatsRefresh:
		return handleStatsRefreshCallback(b, ctx, cbData)
	case GTCallbackStatsBack:
		return handleStatsBackCallback(b, ctx, cbData)
	case GTCallbackInlineStats:
		return handleInlineStatsCallback(b, ctx, cbData)
	case GTCallbackBackToEditList:
		return handleBackToEditListCallback(b, ctx, cbData)
	case GTCallbackBackToEditGameList:
		return handleBackToEditGameListCallback(b, ctx, cbData)
	case GTCallbackEditGameList:
		return handleEditGameListCallback(b, ctx, cbData)
	}

	return nil
}

func parseGameTrackingCallback(data string) (GameTrackingCallbackData, error) {
	result := GameTrackingCallbackData{}

	prefixes := map[string]GameTrackingCallbackType{
		"gt_add:":                GTCallbackAddGame,
		"gt_add_custom:":         GTCallbackAddCustomGame,
		"gt_remove:":             GTCallbackRemoveGame,
		"gt_confirm_remove:":     GTCallbackConfirmRemove,
		"gt_cancel_remove:":      GTCallbackCancelRemove,
		"gt_edit:":               GTCallbackEditGame,
		"gt_edit_status:":        GTCallbackEditStatus,
		"gt_edit_hours:":         GTCallbackEditHours,
		"gt_edit_fav:":           GTCallbackEditFavorite,
		"gt_edit_rating:":        GTCallbackEditRating,
		"gt_edit_notes:":         GTCallbackEditNotes,
		"gt_status_set:":         GTCallbackStatusSet,
		"gt_rating_set:":         GTCallbackRatingSet,
		"gt_import_all:":         GTCallbackImportAll,
		"gt_import_played:":      GTCallbackImportPlayed,
		"gt_list_all:":           GTCallbackListGames,
		"gt_list_fav:":           GTCallbackListFavorites,
		"gt_list_comp:":          GTCallbackListCompleted,
		"gt_list_backlog:":       GTCallbackListBacklog,
		"gt_list_playing:":       GTCallbackListPlaying,
		"gt_stats_refresh:":      GTCallbackStatsRefresh,
		"gt_stats_back:":         GTCallbackStatsBack,
		"gt_inline_stats:":       GTCallbackInlineStats,
		"gt_back_edit_list:":     GTCallbackBackToEditList,
		"gt_back_edit_gamelist:": GTCallbackBackToEditGameList,
		"gt_edit_list:":          GTCallbackEditGameList,
	}

	var payload string
	for prefix, cbType := range prefixes {
		if strings.HasPrefix(data, prefix) {
			result.Type = cbType
			payload = strings.TrimPrefix(data, prefix)
			break
		}
	}

	if result.Type == GTCallbackUnknown {
		return result, fmt.Errorf("unknown callback type")
	}

	// Parse payload based on type
	parts := strings.Split(payload, "_")

	if len(parts) > 0 && parts[0] != "" {
		switch result.Type {
		case GTCallbackAddGame, GTCallbackAddCustomGame:
			result.AppID = parts[0]
			if len(parts) > 1 {
				result.UserID, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		case GTCallbackEditGameList, GTCallbackBackToEditGameList,
			GTCallbackListGames, GTCallbackListFavorites, GTCallbackListCompleted,
			GTCallbackListBacklog, GTCallbackListPlaying:
			// Format: PAGE_USERID
			result.Page, _ = strconv.Atoi(parts[0])
			if len(parts) > 1 {
				result.UserID, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		default:
			// For other callbacks, first part is game ID
			result.GameID, _ = strconv.ParseInt(parts[0], 10, 64)
			if len(parts) > 1 {
				result.UserID, _ = strconv.ParseInt(parts[1], 10, 64)
			}
			if len(parts) > 2 {
				result.Page, _ = strconv.Atoi(parts[2])
			}
			if len(parts) > 3 {
				result.Extra = parts[3]
			}
		}
	}

	return result, nil
}

// ========== /addgame Command ==========

// parseAddGameFlags parses optional flags from addgame command
// Returns: gameName, status, playtime, error
func parseAddGameFlags(args string) (string, *steam.GameStatus, *float64, error) {
	var status *steam.GameStatus
	var playtime *float64
	gameName := args

	// Check for -s or --status flag
	statusPatterns := []string{" -s ", " --status "}
	for _, pattern := range statusPatterns {
		if idx := strings.Index(strings.ToLower(args), pattern); idx != -1 {
			// Find the status value
			rest := args[idx+len(pattern):]
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) > 0 && parts[0] != "" {
				statusVal := strings.ToLower(strings.TrimSpace(parts[0]))
				var s steam.GameStatus
				switch statusVal {
				case "completed", "c":
					s = steam.StatusCompleted
				case "playing", "p":
					s = steam.StatusPlaying
				case "notstarted", "not_started", "ns", "n":
					s = steam.StatusNotStarted
				case "onhold", "on_hold", "oh", "h":
					s = steam.StatusOnHold
				case "dropped", "d":
					s = steam.StatusDropped
				case "paused", "pa":
					s = steam.StatusPaused
				default:
					return "", nil, nil, fmt.Errorf("invalid status: %s", statusVal)
				}
				status = &s
				// Remove flag from game name
				gameName = strings.TrimSpace(args[:idx])
				if len(parts) > 1 {
					gameName = strings.TrimSpace(gameName + " " + parts[1])
				}
				args = gameName
			}
		}
	}

	// Check for -t or --time flag (playtime in hours)
	timePatterns := []string{" -t ", " --time "}
	for _, pattern := range timePatterns {
		if idx := strings.Index(strings.ToLower(args), pattern); idx != -1 {
			rest := args[idx+len(pattern):]
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) > 0 && parts[0] != "" {
				hours, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
				if err != nil {
					return "", nil, nil, fmt.Errorf("invalid playtime: %s", parts[0])
				}
				playtime = &hours
				// Remove flag from game name
				gameName = strings.TrimSpace(args[:idx])
				if len(parts) > 1 {
					gameName = strings.TrimSpace(gameName + " " + parts[1])
				}
				args = gameName
			}
		}
	}

	return strings.TrimSpace(gameName), status, playtime, nil
}

func HandleAddGameCommand(b *gotgbot.Bot, ctx *ext.Context, cfg *config.Config) error {
	args := extractCommandArgs(ctx.EffectiveMessage.Text, "addgame")

	if args == "" {
		_, err := ctx.EffectiveMessage.Reply(b, templates.GameTrackingHelp,
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	// Parse flags
	gameName, flagStatus, flagPlaytime, err := parseAddGameFlags(args)
	if err != nil {
		_, err := ctx.EffectiveMessage.Reply(b,
			fmt.Sprintf("Error parsing flags: %s\n\nUse /addgame without arguments for help.", err.Error()),
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	if gameName == "" {
		_, err := ctx.EffectiveMessage.Reply(b,
			"Please provide a game name.\n\nUse /addgame without arguments for help.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	// Store flags in context for later use in callback
	addGameFlags := &AddGameFlags{Status: flagStatus, Playtime: flagPlaytime}
	storeAddGameFlags(ctx.EffectiveMessage.From.Id, addGameFlags)

	// Search Steam for the game
	results, err := steam.SearchSteam(gameName)
	if err != nil || len(results) == 0 {
		// No Steam results - offer to add as custom game
		return sendAddCustomGamePrompt(b, ctx, gameName)
	}

	// Show first result with Add button and "Add Custom" option
	firstResult := results[0]
	appID := strconv.Itoa(firstResult.ID)
	userID := ctx.EffectiveMessage.From.Id

	// Check if already in library
	db := steam.GetDatabase()
	exists, _ := db.UserGameExists(context.Background(), userID, appID)

	if exists {
		_, err := ctx.EffectiveMessage.Reply(b,
			fmt.Sprintf("<b>%s</b> is already in your library.", firstResult.Name),
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	// Get full app info
	appInfo, _ := steam.GetSteamAppInfo(appID)

	usPrice := float64(firstResult.Price.Final) / 100.0
	priceDisplay := formatPriceDisplay(usPrice, appInfo.Price)

	msg := fmt.Sprintf(
		"<b>%s</b>\n\n"+
			"› <b>Price:</b> %s\n"+
			"<a href='%s'>&#xad;</a>\n"+
			"<i>%s</i>\n\n"+
			"Add this game to your library?",
		firstResult.Name,
		priceDisplay,
		firstNonEmpty(appInfo.HeaderImage, firstResult.TinyImage),
		truncate(appInfo.Description, 300),
	)

	switchQuery := gameName
	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "+ Add to Library", CallbackData: fmt.Sprintf("gt_add:%s_%d", appID, userID)},
			},
			{
				{Text: "More Results", SwitchInlineQueryCurrentChat: &switchQuery},
			},
			{
				{Text: "Add Custom Game", CallbackData: fmt.Sprintf("gt_add_custom:%s_%d", args, userID)},
			},
		},
	}

	_, err = ctx.EffectiveMessage.Reply(b, msg, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
	return err
}

func sendAddCustomGamePrompt(b *gotgbot.Bot, ctx *ext.Context, gameName string) error {
	userID := ctx.EffectiveMessage.From.Id

	msg := fmt.Sprintf(
		"No Steam game found for <b>\"%s\"</b>\n\n"+
			"Would you like to add it as a custom game?\n"+
			"(Perfect for PlayStation, Xbox, mobile games, etc.)",
		gameName,
	)

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "Add as Custom Game", CallbackData: fmt.Sprintf("gt_add_custom:%s_%d", gameName, userID)},
			},
		},
	}

	_, err := ctx.EffectiveMessage.Reply(b, msg, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
	return err
}

func handleAddGameCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData, cfg *config.Config) error {
	db := steam.GetDatabase()
	if db == nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Database not available",
			ShowAlert: true,
		})
		return nil
	}

	// Check if already exists (fast check)
	exists, _ := db.UserGameExists(context.Background(), cbData.UserID, cbData.AppID)
	if exists {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Already in your library",
			ShowAlert: true,
		})
		return nil
	}

	// Answer immediately with success message
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "Game added to library",
	})

	// Get Steam app details (cached)
	details, err := steam.GetFullSteamAppDetails(cbData.AppID)
	if err != nil {
		log.Printf("Error getting app details: %v", err)
		errMsg := "Error fetching game details. Please try again."
		if ctx.CallbackQuery.InlineMessageId != "" {
			_, _, _ = b.EditMessageText(errMsg, &gotgbot.EditMessageTextOpts{
				InlineMessageId: ctx.CallbackQuery.InlineMessageId,
				ParseMode:       "HTML",
			})
		} else if ctx.CallbackQuery.Message != nil {
			_, _, _ = ctx.CallbackQuery.Message.EditText(b, errMsg, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		}
		return nil
	}

	// Try to get HLTB data for default playtime
	hltbData, _ := steam.GetHltbData(context.Background(), cbData.AppID, details.Name)

	// Create game entry
	// Get any flags that were set during the command
	flags := getAndClearAddGameFlags(cbData.UserID)

	// Determine status (flag > default completed)
	gameStatus := steam.StatusCompleted
	if flags != nil && flags.Status != nil {
		gameStatus = *flags.Status
	}

	// Determine playtime (flag > HLTB > 0)
	gamePlaytime := float64(hltbData.MainStory)
	if flags != nil && flags.Playtime != nil {
		gamePlaytime = *flags.Playtime
	}

	game := &steam.UserGame{
		UserID:      cbData.UserID,
		AppID:       sql.NullString{String: cbData.AppID, Valid: true},
		GameName:    details.Name,
		Status:      gameStatus,
		TimePlayed:  gamePlaytime,
		IsFavorite:  false,
		IsSteamGame: true,
	}

	// Add header image
	if details.HeaderImage != "" {
		game.HeaderImage = sql.NullString{String: details.HeaderImage, Valid: true}
	}

	// Add price info
	if details.PriceOverview.FinalFormatted != "" {
		game.PriceUSD = sql.NullInt64{Int64: int64(details.PriceOverview.Final), Valid: true}
	}

	// Add HLTB data
	if hltbData.MainStory > 0 {
		game.HLTBMain = sql.NullFloat64{Float64: float64(hltbData.MainStory), Valid: true}
	}
	if hltbData.MainPlusExtra > 0 {
		game.HLTBExtra = sql.NullFloat64{Float64: float64(hltbData.MainPlusExtra), Valid: true}
	}
	if hltbData.Completionist > 0 {
		game.HLTBCompletionist = sql.NullFloat64{Float64: float64(hltbData.Completionist), Valid: true}
	}

	// Add to database
	err = db.AddUserGame(context.Background(), game)
	if err != nil {
		log.Printf("Error adding game: %v", err)
		errMsg := "Error adding game to library. Please try again."
		if ctx.CallbackQuery.InlineMessageId != "" {
			_, _, _ = b.EditMessageText(errMsg, &gotgbot.EditMessageTextOpts{
				InlineMessageId: ctx.CallbackQuery.InlineMessageId,
				ParseMode:       "HTML",
			})
		} else if ctx.CallbackQuery.Message != nil {
			_, _, _ = ctx.CallbackQuery.Message.EditText(b, errMsg, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		}
		return nil
	}

	// Get the inserted game ID to build the remove button
	insertedGame, _ := db.GetUserGameByAppID(context.Background(), cbData.UserID, cbData.AppID)

	// Update button to "Remove from Library" if this is from inline query
	if ctx.CallbackQuery.InlineMessageId != "" {
		// This is from an inline query result
		userID := cbData.UserID

		// Build updated keyboard with "Remove from Library" button
		replyMarkup := &gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{Text: "View on Steam", Url: fmt.Sprintf("https://store.steampowered.com/app/%s", cbData.AppID)},
					{Text: "SteamDB", Url: fmt.Sprintf("https://steamdb.info/app/%s/", cbData.AppID)},
				},
				{
					{Text: "Details", CallbackData: fmt.Sprintf("details:%s_%d", cbData.AppID, userID)},
					{Text: "Requirements", CallbackData: fmt.Sprintf("requirements:%s_%d", cbData.AppID, userID)},
				},
				{
					{Text: "− Remove from Library", CallbackData: fmt.Sprintf("gt_remove:%d_%d", insertedGame.ID, userID)},
				},
			},
		}

		// Edit the inline message's reply markup
		_, _, _ = b.EditMessageReplyMarkup(&gotgbot.EditMessageReplyMarkupOpts{
			InlineMessageId: ctx.CallbackQuery.InlineMessageId,
			ReplyMarkup:     *replyMarkup,
		})
	} else if ctx.CallbackQuery.Message != nil {
		// This is from a regular chat message (from /addgame command)
		msg := fmt.Sprintf(
			"<b>%s</b> added to your library!\n\n"+
				"› Status: %s %s\n"+
				"› Time: %.1fh\n\n"+
				"Use /mygamestats to view your library stats!",
			details.Name,
			getStatusSymbol(game.Status), game.Status.DisplayName(),
			game.TimePlayed,
		)

		_, _, _ = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	}

	return nil
}

func handleAddCustomGameCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData, cfg *config.Config) error {
	db := steam.GetDatabase()
	if db == nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Database not available",
			ShowAlert: true,
		})
		return nil
	}

	gameName := cbData.AppID // For custom games, AppID field holds the game name

	// Check if already exists by name
	exists, _ := db.UserGameExistsByName(context.Background(), cbData.UserID, gameName)
	if exists {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Already in your library",
			ShowAlert: true,
		})
		return nil
	}

	// Answer immediately with success message
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "Custom game added to library",
	})

	// Try to get HLTB data even for non-Steam games (HLTB searches by name)
	hltbData, _ := steam.GetHltbData(context.Background(), "", gameName)

	// Create game entry
	// Get any flags that were set during the command
	flags := getAndClearAddGameFlags(cbData.UserID)

	// Determine status (flag > default completed)
	gameStatus := steam.StatusCompleted
	if flags != nil && flags.Status != nil {
		gameStatus = *flags.Status
	}

	// Determine playtime (flag > HLTB > 0)
	gamePlaytime := float64(hltbData.MainStory)
	if flags != nil && flags.Playtime != nil {
		gamePlaytime = *flags.Playtime
	}

	game := &steam.UserGame{
		UserID:      cbData.UserID,
		AppID:       sql.NullString{Valid: false}, // NULL for non-Steam games
		GameName:    gameName,
		Status:      gameStatus,
		TimePlayed:  gamePlaytime,
		IsFavorite:  false,
		IsSteamGame: false,
	}

	// Add HLTB data if available
	if hltbData.MainStory > 0 {
		game.HLTBMain = sql.NullFloat64{Float64: float64(hltbData.MainStory), Valid: true}
	}
	if hltbData.MainPlusExtra > 0 {
		game.HLTBExtra = sql.NullFloat64{Float64: float64(hltbData.MainPlusExtra), Valid: true}
	}
	if hltbData.Completionist > 0 {
		game.HLTBCompletionist = sql.NullFloat64{Float64: float64(hltbData.Completionist), Valid: true}
	}

	// Add to database
	err := db.AddUserGame(context.Background(), game)
	if err != nil {
		log.Printf("Error adding custom game: %v", err)
		errMsg := "Error adding game to library. Please try again."
		if ctx.CallbackQuery.InlineMessageId != "" {
			_, _, _ = b.EditMessageText(errMsg, &gotgbot.EditMessageTextOpts{
				InlineMessageId: ctx.CallbackQuery.InlineMessageId,
				ParseMode:       "HTML",
			})
		} else if ctx.CallbackQuery.Message != nil {
			_, _, _ = ctx.CallbackQuery.Message.EditText(b, errMsg, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		}
		return nil
	}

	// Only edit message text if this is from a regular message (not inline query)
	if ctx.CallbackQuery.Message != nil {
		msg := fmt.Sprintf(
			"<b>%s</b> added to your library!\n\n"+
				"› Status: %s %s\n"+
				"› Time: %.1fh\n"+
				"› Type: Custom Game\n\n"+
				"Use /mygamestats to view your library stats!",
			gameName,
			getStatusSymbol(game.Status), game.Status.DisplayName(),
			game.TimePlayed,
		)

		_, _, _ = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	}

	return nil
}

// ========== /removegame Command ==========

func HandleRemoveGameCommand(b *gotgbot.Bot, ctx *ext.Context, cfg *config.Config) error {
	userID := ctx.EffectiveMessage.From.Id
	db := steam.GetDatabase()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	// Get user's games (first 10)
	games, err := db.GetUserGames(context.Background(), userID, nil, false, 10, 0)
	if err != nil {
		log.Printf("Error getting user games: %v", err)
		_, err := ctx.EffectiveMessage.Reply(b,
			"Error fetching your library. Please try again.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	if len(games) == 0 {
		_, err := ctx.EffectiveMessage.Reply(b,
			"Your library is empty!\n\nUse /addgame to add games.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	return sendRemoveGameList(b, ctx, games, 0, userID)
}

func sendRemoveGameList(b *gotgbot.Bot, ctx *ext.Context, games []steam.UserGame, page int, userID int64) error {
	var msg strings.Builder
	msg.WriteString("<b>Remove Game from Library</b>\n\n")
	msg.WriteString("Select a game to remove:\n\n")

	keyboard := make([][]gotgbot.InlineKeyboardButton, 0)

	for _, game := range games {
		statusSymbol := getStatusSymbol(game.Status)
		gameText := fmt.Sprintf("%s %s", statusSymbol, game.GameName)
		if len(gameText) > 50 {
			gameText = gameText[:47] + "..."
		}

		keyboard = append(keyboard, []gotgbot.InlineKeyboardButton{
			{Text: gameText, CallbackData: fmt.Sprintf("gt_remove:%d_%d", game.ID, userID)},
		})
	}

	// TODO: Add pagination if needed

	_, err := ctx.EffectiveMessage.Reply(b, msg.String(), &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyboard},
	})
	return err
}

func handleRemoveGameCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Confirm removal..."})

	msg := fmt.Sprintf(
		"<b>Confirm Removal</b>\n\n"+
			"Remove <b>%s</b> from your library?\n\n"+
			"This action cannot be undone.",
		game.GameName,
	)

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "Yes, Remove", CallbackData: fmt.Sprintf("gt_confirm_remove:%d_%d", game.ID, cbData.UserID)},
				{Text: "Cancel", CallbackData: fmt.Sprintf("gt_cancel_remove:%d_%d", game.ID, cbData.UserID)},
			},
		},
	}

	if ctx.CallbackQuery.Message != nil {
		_, _, err = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: keyboard,
		})
	}
	return err
}

func handleConfirmRemoveCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	gameName := game.GameName

	err = db.RemoveUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		log.Printf("Error removing game: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error removing game",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: fmt.Sprintf("%s removed", gameName),
	})

	msg := fmt.Sprintf("<b>%s</b> removed from your library.", gameName)
	if ctx.CallbackQuery.Message != nil {
		_, _, _ = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	}
	return nil
}

func handleCancelRemoveCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Cancelled"})

	if ctx.CallbackQuery.Message != nil {
		_, _, _ = ctx.CallbackQuery.Message.EditText(b,
			"Removal cancelled.",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	}
	return nil
}

// ========== /editgame Command ==========

func HandleEditGameCommand(b *gotgbot.Bot, ctx *ext.Context, cfg *config.Config) error {
	userID := ctx.EffectiveMessage.From.Id
	db := steam.GetDatabase()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	// Get user's games (first page)
	const pageSize = 8
	games, err := db.GetUserGames(context.Background(), userID, nil, false, pageSize+1, 0)
	if err != nil {
		log.Printf("Error getting user games: %v", err)
		_, err := ctx.EffectiveMessage.Reply(b,
			"Error fetching your library. Please try again.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	if len(games) == 0 {
		_, err := ctx.EffectiveMessage.Reply(b,
			"Your library is empty!\n\nUse /addgame to add games.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	hasNextPage := len(games) > pageSize
	if hasNextPage {
		games = games[:pageSize]
	}

	return sendEditGameList(b, ctx, games, userID, 0, hasNextPage)
}

func sendEditGameList(b *gotgbot.Bot, ctx *ext.Context, games []steam.UserGame, userID int64, page int, hasNextPage bool) error {
	var msg strings.Builder
	msg.WriteString("<b>Edit Game</b>\n\n")
	msg.WriteString("Select a game to edit:\n")
	fmt.Fprintf(&msg, "Page %d\n\n", page+1)

	keyboard := make([][]gotgbot.InlineKeyboardButton, 0)

	for _, game := range games {
		statusSymbol := getStatusSymbol(game.Status)
		gameText := fmt.Sprintf("%s %s", statusSymbol, game.GameName)
		if len(gameText) > 40 {
			gameText = gameText[:37] + "..."
		}

		keyboard = append(keyboard, []gotgbot.InlineKeyboardButton{
			{Text: gameText, CallbackData: fmt.Sprintf("gt_edit:%d_%d", game.ID, userID)},
		})
	}

	// Add pagination row
	var navRow []gotgbot.InlineKeyboardButton
	if page > 0 {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❮",
			CallbackData: fmt.Sprintf("gt_edit_list:%d_%d", page-1, userID),
		})
	}
	if hasNextPage {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❯",
			CallbackData: fmt.Sprintf("gt_edit_list:%d_%d", page+1, userID),
		})
	}
	if len(navRow) > 0 {
		keyboard = append(keyboard, navRow)
	}

	_, err := ctx.EffectiveMessage.Reply(b, msg.String(), &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyboard},
	})
	return err
}

func handleEditGameCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Loading..."})

	return sendGameEditOptions(b, ctx, game)
}

func sendGameEditOptions(b *gotgbot.Bot, ctx *ext.Context, game *steam.UserGame) error {
	favIcon := "☆"
	if game.IsFavorite {
		favIcon = "★"
	}

	ratingText := "-"
	if game.Rating.Valid {
		ratingText = fmt.Sprintf("%d/10", game.Rating.Int64)
	}

	notesPreview := "-"
	if game.Notes.Valid && game.Notes.String != "" {
		notesPreview = game.Notes.String
		if len(notesPreview) > 50 {
			notesPreview = notesPreview[:47] + "..."
		}
	}

	statusSymbol := getStatusSymbol(game.Status)

	msg := fmt.Sprintf(
		"<b>Edit: %s</b>\n\n"+
			"› <b>Status:</b> %s %s\n"+
			"› <b>Hours:</b> %.1fh\n"+
			"› <b>Favorite:</b> %s %s\n"+
			"› <b>Rating:</b> %s\n"+
			"› <b>Notes:</b> %s\n\n"+
			"Select a field to edit:",
		game.GameName,
		statusSymbol, game.Status.DisplayName(),
		game.TimePlayed,
		favIcon, boolToYesNo(game.IsFavorite),
		ratingText,
		notesPreview,
	)

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "Status", CallbackData: fmt.Sprintf("gt_edit_status:%d_%d", game.ID, game.UserID)},
				{Text: "Hours", CallbackData: fmt.Sprintf("gt_edit_hours:%d_%d", game.ID, game.UserID)},
			},
			{
				{Text: "★ Favorite", CallbackData: fmt.Sprintf("gt_edit_fav:%d_%d", game.ID, game.UserID)},
				{Text: "Rating", CallbackData: fmt.Sprintf("gt_edit_rating:%d_%d", game.ID, game.UserID)},
			},
			{
				{Text: "Notes", CallbackData: fmt.Sprintf("gt_edit_notes:%d_%d", game.ID, game.UserID)},
			},
			{
				{Text: "❮", CallbackData: fmt.Sprintf("gt_back_edit_gamelist:_%d", game.UserID)},
			},
		},
	}

	if ctx.CallbackQuery != nil && ctx.CallbackQuery.Message != nil {
		_, _, err := ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: keyboard,
		})
		return err
	}

	_, err := ctx.EffectiveMessage.Reply(b, msg, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
	return err
}

func boolToYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func handleEditStatusCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Select status..."})

	statusSymbol := getStatusSymbol(game.Status)
	msg := fmt.Sprintf(
		"<b>Change Status: %s</b>\n\n"+
			"Current: %s %s\n\n"+
			"Select new status:",
		game.GameName,
		statusSymbol, game.Status.DisplayName(),
	)

	statuses := []steam.GameStatus{
		steam.StatusCompleted,
		steam.StatusPlaying,
		steam.StatusNotStarted,
		steam.StatusOnHold,
		steam.StatusPaused,
		steam.StatusDropped,
	}

	keyboard := make([][]gotgbot.InlineKeyboardButton, 0)
	for i := 0; i < len(statuses); i += 2 {
		row := []gotgbot.InlineKeyboardButton{
			{
				Text:         getStatusSymbol(statuses[i]) + " " + statuses[i].DisplayName(),
				CallbackData: fmt.Sprintf("gt_status_set:%d_%d_0_%s", game.ID, game.UserID, statuses[i]),
			},
		}
		if i+1 < len(statuses) {
			row = append(row, gotgbot.InlineKeyboardButton{
				Text:         getStatusSymbol(statuses[i+1]) + " " + statuses[i+1].DisplayName(),
				CallbackData: fmt.Sprintf("gt_status_set:%d_%d_0_%s", game.ID, game.UserID, statuses[i+1]),
			})
		}
		keyboard = append(keyboard, row)
	}
	// Add back button
	keyboard = append(keyboard, []gotgbot.InlineKeyboardButton{
		{Text: "❮", CallbackData: fmt.Sprintf("gt_back_edit_list:%d_%d", game.ID, game.UserID)},
	})

	if ctx.CallbackQuery.Message != nil {
		_, _, err = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode: "HTML",
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: keyboard,
			},
		})
	}
	return err
}

func handleEditFavoriteCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	// Toggle favorite
	newFav := !game.IsFavorite
	err = db.UpdateUserGameFavorite(context.Background(), cbData.UserID, cbData.GameID, newFav)
	if err != nil {
		log.Printf("Error updating favorite: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error updating favorite",
			ShowAlert: true,
		})
		return nil
	}

	favText := "added to"
	if !newFav {
		favText = "removed from"
	}
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: fmt.Sprintf("★ %s favorites", favText),
	})

	game.IsFavorite = newFav
	return sendGameEditOptions(b, ctx, game)
}

func handleEditHoursCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: ""})

	msg := fmt.Sprintf(
		"<b>Edit Hours: %s</b>\n\n"+
			"Current: <code>%.1fh</code>\n\n"+
			"Reply to this message with the new hours played.\n"+
			"Example: <code>25.5</code>",
		game.GameName,
		game.TimePlayed,
	)

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "❮", CallbackData: fmt.Sprintf("gt_back_edit_list:%d_%d", game.ID, game.UserID)}},
		},
	}

	if ctx.CallbackQuery.Message != nil {
		_, _, err = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: keyboard,
		})
		if err == nil {
			// Store pending edit for reply handling
			StorePendingEdit(ctx.CallbackQuery.Message.GetMessageId(), &PendingEdit{
				Type:      PendingEditHours,
				GameID:    game.ID,
				UserID:    game.UserID,
				GameName:  game.GameName,
				ChatID:    ctx.CallbackQuery.Message.GetChat().Id,
				MessageID: ctx.CallbackQuery.Message.GetMessageId(),
				CreatedAt: time.Now(),
			})
		}
	}
	return err
}

func handleEditRatingCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: ""})

	currentRating := "Not rated"
	if game.Rating.Valid {
		currentRating = fmt.Sprintf("%d/10", game.Rating.Int64)
	}

	msg := fmt.Sprintf(
		"<b>Rate: %s</b>\n\n"+
			"Current: %s\n\n"+
			"Select a rating:",
		game.GameName,
		currentRating,
	)

	// Create rating buttons 1-10 in two rows
	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "1", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_1", game.ID, game.UserID)},
				{Text: "2", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_2", game.ID, game.UserID)},
				{Text: "3", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_3", game.ID, game.UserID)},
				{Text: "4", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_4", game.ID, game.UserID)},
				{Text: "5", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_5", game.ID, game.UserID)},
			},
			{
				{Text: "6", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_6", game.ID, game.UserID)},
				{Text: "7", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_7", game.ID, game.UserID)},
				{Text: "8", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_8", game.ID, game.UserID)},
				{Text: "9", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_9", game.ID, game.UserID)},
				{Text: "10", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_10", game.ID, game.UserID)},
			},
			{
				{Text: "Clear Rating", CallbackData: fmt.Sprintf("gt_rating_set:%d_%d_0_0", game.ID, game.UserID)},
			},
			{
				{Text: "❮", CallbackData: fmt.Sprintf("gt_back_edit_list:%d_%d", game.ID, game.UserID)},
			},
		},
	}

	if ctx.CallbackQuery.Message != nil {
		_, _, err = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: keyboard,
		})
	}
	return err
}

func handleEditNotesCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: ""})

	currentNotes := "None"
	if game.Notes.Valid && game.Notes.String != "" {
		currentNotes = game.Notes.String
	}

	msg := fmt.Sprintf(
		"<b>Edit Notes: %s</b>\n\n"+
			"Current notes:\n<i>%s</i>\n\n"+
			"Reply to this message with new notes.",
		game.GameName,
		currentNotes,
	)

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "❮", CallbackData: fmt.Sprintf("gt_back_edit_list:%d_%d", game.ID, game.UserID)}},
		},
	}

	if ctx.CallbackQuery.Message != nil {
		_, _, err = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: keyboard,
		})
		if err == nil {
			// Store pending edit for reply handling
			StorePendingEdit(ctx.CallbackQuery.Message.GetMessageId(), &PendingEdit{
				Type:      PendingEditNotes,
				GameID:    game.ID,
				UserID:    game.UserID,
				GameName:  game.GameName,
				ChatID:    ctx.CallbackQuery.Message.GetChat().Id,
				MessageID: ctx.CallbackQuery.Message.GetMessageId(),
				CreatedAt: time.Now(),
			})
		}
	}
	return err
}

// HandleGameEditReply handles replies to edit hours/notes messages
func HandleGameEditReply(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg == nil || msg.ReplyToMessage == nil {
		return nil
	}

	// Check if the reply is to one of our pending edit messages
	pendingEdit := GetPendingEdit(msg.ReplyToMessage.MessageId)
	if pendingEdit == nil {
		return nil
	}

	// Verify it's the same user
	if msg.From.Id != pendingEdit.UserID {
		return nil
	}

	db := steam.GetDatabase()
	text := strings.TrimSpace(msg.Text)

	switch pendingEdit.Type {
	case PendingEditHours:
		// Parse hours
		hours, err := strconv.ParseFloat(text, 64)
		if err != nil || hours < 0 {
			_, _ = msg.Reply(b, "Invalid hours value. Please enter a valid number (e.g., 25.5)", nil)
			// Re-store the pending edit so user can try again
			StorePendingEdit(pendingEdit.MessageID, pendingEdit)
			return nil
		}

		err = db.UpdateUserGameTimePlayed(context.Background(), pendingEdit.UserID, pendingEdit.GameID, hours)
		if err != nil {
			_, _ = msg.Reply(b, "Failed to update hours.", nil)
			return err
		}

		_, _ = msg.Reply(b, fmt.Sprintf("Updated <b>%s</b> to <code>%.1fh</code>", pendingEdit.GameName, hours), &gotgbot.SendMessageOpts{
			ParseMode: "HTML",
		})

	case PendingEditNotes:
		err := db.UpdateUserGameNotes(context.Background(), pendingEdit.UserID, pendingEdit.GameID, text)
		if err != nil {
			_, _ = msg.Reply(b, "Failed to update notes.", nil)
			return err
		}

		notesPreview := text
		if len(notesPreview) > 50 {
			notesPreview = notesPreview[:47] + "..."
		}
		_, _ = msg.Reply(b, fmt.Sprintf("Updated notes for <b>%s</b>", pendingEdit.GameName), &gotgbot.SendMessageOpts{
			ParseMode: "HTML",
		})
	}

	return nil
}

func handleBackToEditListCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Game not found",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: ""})
	return sendGameEditOptions(b, ctx, game)
}

// handleBackToEditGameListCallback returns to the edit game list (page 0)
func handleBackToEditGameListCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	cbData.Page = 0
	return handleEditGameListCallback(b, ctx, cbData)
}

// handleEditGameListCallback handles pagination for edit game list
func handleEditGameListCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	const pageSize = 8
	games, err := db.GetUserGames(context.Background(), cbData.UserID, nil, false, pageSize+1, cbData.Page*pageSize)
	if err != nil {
		log.Printf("Error getting user games: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error loading games",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: ""})

	hasNextPage := len(games) > pageSize
	if hasNextPage {
		games = games[:pageSize]
	}

	var msg strings.Builder
	msg.WriteString("<b>Edit Game</b>\n\n")
	msg.WriteString("Select a game to edit:\n")
	fmt.Fprintf(&msg, "Page %d\n\n", cbData.Page+1)

	keyboard := make([][]gotgbot.InlineKeyboardButton, 0)

	for _, game := range games {
		statusSymbol := getStatusSymbol(game.Status)
		gameText := fmt.Sprintf("%s %s", statusSymbol, game.GameName)
		if len(gameText) > 40 {
			gameText = gameText[:37] + "..."
		}
		keyboard = append(keyboard, []gotgbot.InlineKeyboardButton{
			{Text: gameText, CallbackData: fmt.Sprintf("gt_edit:%d_%d", game.ID, game.UserID)},
		})
	}

	// Add pagination row
	var navRow []gotgbot.InlineKeyboardButton
	if cbData.Page > 0 {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❮",
			CallbackData: fmt.Sprintf("gt_edit_list:%d_%d", cbData.Page-1, cbData.UserID),
		})
	}
	if hasNextPage {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❯",
			CallbackData: fmt.Sprintf("gt_edit_list:%d_%d", cbData.Page+1, cbData.UserID),
		})
	}
	if len(navRow) > 0 {
		keyboard = append(keyboard, navRow)
	}

	if ctx.CallbackQuery.Message != nil {
		_, _, err = ctx.CallbackQuery.Message.EditText(b, msg.String(), &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyboard},
		})
		return err
	}
	return nil
}

// ========== /mygamestats Command ==========

// buildGameStatsMessage creates the stats message and keyboard for a user
func buildGameStatsMessage(stats *steam.UserGameStats, userID int64, includeTimestamp bool) (string, gotgbot.InlineKeyboardMarkup) {
	var msg strings.Builder
	msg.WriteString("<b>Your Gaming Stats</b>\n\n")

	// Overview with minimal symbols
	fmt.Fprintf(&msg, "› <b>Total:</b> %d games\n", stats.TotalGames)
	fmt.Fprintf(&msg, "› <b>Playtime:</b> %.1f hours\n", stats.TotalPlaytime)
	if stats.TotalFavorites > 0 {
		fmt.Fprintf(&msg, "› <b>Favorites:</b> %d games\n", stats.TotalFavorites)
	}

	msg.WriteString("\n<b>Status Breakdown:</b>\n")
	if stats.CompletedCount > 0 {
		fmt.Fprintf(&msg, "◦ Completed: %d games (%.1fh)\n", stats.CompletedCount, stats.CompletedHours)
	}
	if stats.PlayingCount > 0 {
		fmt.Fprintf(&msg, "◦ Playing: %d games\n", stats.PlayingCount)
	}
	if stats.BacklogCount > 0 {
		fmt.Fprintf(&msg, "◦ Backlog: %d games\n", stats.BacklogCount)
		if stats.NotStartedCount > 0 {
			fmt.Fprintf(&msg, "  └ Not Started: %d\n", stats.NotStartedCount)
		}
		if stats.OnHoldCount > 0 {
			fmt.Fprintf(&msg, "  └ On Hold: %d\n", stats.OnHoldCount)
		}
		if stats.DroppedCount > 0 {
			fmt.Fprintf(&msg, "  └ Dropped: %d\n", stats.DroppedCount)
		}
		if stats.PausedCount > 0 {
			fmt.Fprintf(&msg, "  └ Paused: %d\n", stats.PausedCount)
		}
	}

	// Top rated games
	if len(stats.TopRated) > 0 {
		msg.WriteString("\n<b>Top Rated:</b>\n")
		for i, game := range stats.TopRated {
			if game.Rating.Valid {
				fmt.Fprintf(&msg, "%d. %s ★ %d/10\n", i+1, game.GameName, game.Rating.Int64)
			}
		}
	}

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "All Games", CallbackData: fmt.Sprintf("gt_list_all:0_%d", userID)},
				{Text: "★ Favorites", CallbackData: fmt.Sprintf("gt_list_fav:0_%d", userID)},
			},
			{
				{Text: "✓ Completed", CallbackData: fmt.Sprintf("gt_list_comp:0_%d", userID)},
				{Text: "Backlog", CallbackData: fmt.Sprintf("gt_list_backlog:0_%d", userID)},
			},
			{
				{Text: "Playing", CallbackData: fmt.Sprintf("gt_list_playing:0_%d", userID)},
				{Text: "↻ Refresh", CallbackData: fmt.Sprintf("gt_stats_refresh:_%d", userID)},
			},
		},
	}

	msgStr := msg.String()
	if includeTimestamp {
		// Add invisible zero-width spaces to force message update (varying count based on time)
		msgStr += strings.Repeat("\u200B", int(time.Now().UnixNano()%5)+1)
	}

	return msgStr, keyboard
}

func HandleMyGameStatsCommand(b *gotgbot.Bot, ctx *ext.Context, cfg *config.Config) error {
	userID := ctx.EffectiveMessage.From.Id
	db := steam.GetDatabase()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	stats, err := db.GetUserGameStats(context.Background(), userID)
	if err != nil {
		log.Printf("Error getting stats: %v", err)
		_, err := ctx.EffectiveMessage.Reply(b,
			"Error fetching your stats. Please try again.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	if stats.TotalGames == 0 {
		_, err := ctx.EffectiveMessage.Reply(b,
			"Your library is empty!\n\nUse /addgame to add games.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	msg, keyboard := buildGameStatsMessage(stats, userID, false)

	_, err = ctx.EffectiveMessage.Reply(b, msg, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
	return err
}

// handleInlineStatsCallback handles the inline stats button press
func handleInlineStatsCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	if db == nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Database not available",
			ShowAlert: true,
		})
		return nil
	}

	stats, err := db.GetUserGameStats(context.Background(), cbData.UserID)
	if err != nil {
		log.Printf("Error getting stats: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error fetching stats",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: ""})

	if stats.TotalGames == 0 {
		_, _, _ = b.EditMessageText(
			"Your library is empty!\n\nUse /addgame to add games.",
			&gotgbot.EditMessageTextOpts{
				InlineMessageId: ctx.CallbackQuery.InlineMessageId,
				ParseMode:       "HTML",
			})
		return nil
	}

	msg, keyboard := buildGameStatsMessage(stats, cbData.UserID, false)

	_, _, err = b.EditMessageText(msg, &gotgbot.EditMessageTextOpts{
		InlineMessageId: ctx.CallbackQuery.InlineMessageId,
		ParseMode:       "HTML",
		ReplyMarkup:     keyboard,
	})
	return err
}

// handleStatsRefreshCallback refreshes the stats by editing the existing message
func handleStatsRefreshCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	if db == nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Database not available",
			ShowAlert: true,
		})
		return nil
	}

	stats, err := db.GetUserGameStats(context.Background(), cbData.UserID)
	if err != nil {
		log.Printf("Error getting stats: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error fetching stats",
			ShowAlert: true,
		})
		return nil
	}

	if stats.TotalGames == 0 {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Refreshed"})
		_ = editMessageText(b, ctx, "Your library is empty!\n\nUse /addgame to add games.",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Refreshed"})

	msg, keyboard := buildGameStatsMessage(stats, cbData.UserID, true)

	return editMessageText(b, ctx, msg, &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
}

// handleStatsBackCallback returns to the stats view from a list view
func handleStatsBackCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	if db == nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Database not available",
			ShowAlert: true,
		})
		return nil
	}

	stats, err := db.GetUserGameStats(context.Background(), cbData.UserID)
	if err != nil {
		log.Printf("Error getting stats: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error fetching stats",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: ""})

	if stats.TotalGames == 0 {
		_ = editMessageText(b, ctx, "Your library is empty!\n\nUse /addgame to add games.",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return nil
	}

	msg, keyboard := buildGameStatsMessage(stats, cbData.UserID, false)

	return editMessageText(b, ctx, msg, &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
}

func handleListGamesCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData, status *steam.GameStatus, favoritesOnly bool) error {
	db := steam.GetDatabase()
	const pageSize = 10
	games, err := db.GetUserGames(context.Background(), cbData.UserID, status, favoritesOnly, pageSize+1, cbData.Page*pageSize)
	if err != nil {
		log.Printf("Error getting games: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error loading games",
			ShowAlert: true,
		})
		return nil
	}

	if len(games) == 0 && cbData.Page == 0 {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "No games found"})
		if ctx.CallbackQuery.Message != nil {
			keyboard := gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{{Text: "❮", CallbackData: fmt.Sprintf("gt_stats_back:_%d", cbData.UserID)}},
				},
			}
			_, _, _ = ctx.CallbackQuery.Message.EditText(b,
				"No games found in this category.",
				&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: keyboard})
		}
		return nil
	}

	// Check if there's a next page
	hasNextPage := len(games) > pageSize
	if hasNextPage {
		games = games[:pageSize] // Trim to actual page size
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Loading..."})

	var msg strings.Builder
	if favoritesOnly {
		msg.WriteString("<b>★ Favorite Games</b>\n\n")
	} else if status != nil {
		msg.WriteString(fmt.Sprintf("<b>%s Games</b>\n\n", status.DisplayName()))
	} else {
		msg.WriteString("<b>All Games</b>\n\n")
	}

	for _, game := range games {
		statusSymbol := getStatusSymbol(game.Status)
		fav := ""
		if game.IsFavorite {
			fav = " ★"
		}
		msg.WriteString(fmt.Sprintf("%s %s · <code>%.1fh</code>%s\n", statusSymbol, game.GameName, game.TimePlayed, fav))
	}

	// Determine callback prefix for pagination
	var callbackPrefix string
	if favoritesOnly {
		callbackPrefix = "gt_list_fav"
	} else if status != nil {
		switch *status {
		case steam.StatusCompleted:
			callbackPrefix = "gt_list_comp"
		case steam.StatusPlaying:
			callbackPrefix = "gt_list_playing"
		default:
			callbackPrefix = "gt_list_all"
		}
	} else {
		callbackPrefix = "gt_list_all"
	}

	// Build pagination row
	var navRow []gotgbot.InlineKeyboardButton
	if cbData.Page > 0 {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❮",
			CallbackData: fmt.Sprintf("%s:%d_%d", callbackPrefix, cbData.Page-1, cbData.UserID),
		})
	}
	if hasNextPage {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❯",
			CallbackData: fmt.Sprintf("%s:%d_%d", callbackPrefix, cbData.Page+1, cbData.UserID),
		})
	}

	// Build keyboard with pagination row (if any) and stats menu button
	var rows [][]gotgbot.InlineKeyboardButton
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}
	rows = append(rows, []gotgbot.InlineKeyboardButton{
		{Text: "Stats Menu", CallbackData: fmt.Sprintf("gt_stats_back:_%d", cbData.UserID)},
	})

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}

	return editMessageText(b, ctx, msg.String(), &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
}

// getStatusSymbol returns a minimal symbol for game status
func getStatusSymbol(s steam.GameStatus) string {
	switch s {
	case steam.StatusCompleted:
		return "✓"
	case steam.StatusPlaying:
		return "▸"
	case steam.StatusNotStarted:
		return "○"
	case steam.StatusOnHold:
		return "‖"
	case steam.StatusDropped:
		return "×"
	case steam.StatusPaused:
		return "◫"
	}
	return "·"
}

func handleListBacklogCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	const pageSize = 10
	games, err := db.GetUserBacklogGames(context.Background(), cbData.UserID, pageSize+1, cbData.Page*pageSize)
	if err != nil {
		log.Printf("Error getting backlog: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error loading backlog",
			ShowAlert: true,
		})
		return nil
	}

	if len(games) == 0 && cbData.Page == 0 {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "No backlog games"})
		if ctx.CallbackQuery.Message != nil {
			keyboard := gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{{Text: "❮", CallbackData: fmt.Sprintf("gt_stats_back:_%d", cbData.UserID)}},
				},
			}
			_, _, _ = ctx.CallbackQuery.Message.EditText(b,
				"No games in your backlog!",
				&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: keyboard})
		}
		return nil
	}

	// Check if there's a next page
	hasNextPage := len(games) > pageSize
	if hasNextPage {
		games = games[:pageSize]
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Loading..."})

	var msg strings.Builder
	msg.WriteString("<b>Backlog Games</b>\n\n")

	for _, game := range games {
		statusSymbol := getStatusSymbol(game.Status)
		msg.WriteString(fmt.Sprintf("%s %s\n", statusSymbol, game.GameName))
	}

	// Build pagination row
	var navRow []gotgbot.InlineKeyboardButton
	if cbData.Page > 0 {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❮",
			CallbackData: fmt.Sprintf("gt_list_backlog:%d_%d", cbData.Page-1, cbData.UserID),
		})
	}
	navRow = append(navRow, gotgbot.InlineKeyboardButton{
		Text:         "❮",
		CallbackData: fmt.Sprintf("gt_stats_back:_%d", cbData.UserID),
	})
	if hasNextPage {
		navRow = append(navRow, gotgbot.InlineKeyboardButton{
			Text:         "❯",
			CallbackData: fmt.Sprintf("gt_list_backlog:%d_%d", cbData.Page+1, cbData.UserID),
		})
	}

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{navRow},
	}

	return editMessageText(b, ctx, msg.String(), &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
}

// ========== /importsteam Command ==========

func HandleImportSteamCommand(b *gotgbot.Bot, ctx *ext.Context, cfg *config.Config) error {
	if cfg.SteamAPIKey == "" {
		_, err := ctx.EffectiveMessage.Reply(b,
			"Steam API key not configured.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	args := extractCommandArgs(ctx.EffectiveMessage.Text, "importsteam")

	if args == "" {
		_, err := ctx.EffectiveMessage.Reply(b,
			"<b>Import Steam Library</b>\n\n"+
				"Usage: <code>/importsteam username</code>\n\n"+
				"Example: <code>/importsteam gaben</code>",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	// Send initial message
	msg, err := ctx.EffectiveMessage.Reply(b,
		fmt.Sprintf("Fetching Steam library for <b>%s</b>...", args),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	if err != nil {
		return err
	}

	// Resolve username to Steam ID
	steamID, err := steam.ResolveSteamVanityURL(cfg.SteamAPIKey, args)
	if err != nil {
		_, _, _ = msg.EditText(b,
			fmt.Sprintf("User not found: <b>%s</b>", args),
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return nil
	}

	// Get owned games
	gamesResp, err := steam.GetSteamOwnedGames(cfg.SteamAPIKey, steamID)
	if err != nil {
		_, _, _ = msg.EditText(b,
			"Error fetching Steam library. Profile may be private.",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return nil
	}

	if len(gamesResp.Response.Games) == 0 {
		_, _, _ = msg.EditText(b,
			"No games found in this Steam library.",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return nil
	}

	// Show confirmation
	confirmMsg := fmt.Sprintf(
		"Found <b>%d games</b> in Steam library!\n\n"+
			"Import all games?\n\n"+
			"<i>Note: This may take a few moments...</i>",
		len(gamesResp.Response.Games),
	)

	keyboard := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{Text: "Import All", CallbackData: fmt.Sprintf("gt_import_all:%s_%d", steamID, ctx.EffectiveMessage.From.Id)},
			},
			{
				{Text: "▸ Import Played Only", CallbackData: fmt.Sprintf("gt_import_played:%s_%d", steamID, ctx.EffectiveMessage.From.Id)},
			},
		},
	}

	_, _, err = msg.EditText(b, confirmMsg, &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})
	return err
}

func handleStatusSetCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	if db == nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Database not available",
			ShowAlert: true,
		})
		return nil
	}

	// Parse status from Extra field
	newStatus := steam.GameStatus(cbData.Extra)
	if !newStatus.IsValid() {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Invalid status",
			ShowAlert: true,
		})
		return nil
	}

	err := db.UpdateUserGameStatus(context.Background(), cbData.UserID, cbData.GameID, newStatus)
	if err != nil {
		log.Printf("Error updating status: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error updating status",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: fmt.Sprintf("Status updated to %s", newStatus.DisplayName()),
	})

	// Fetch game and show edit options again
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		return nil
	}

	return sendGameEditOptions(b, ctx, game)
}

func handleRatingSetCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData) error {
	db := steam.GetDatabase()
	if db == nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Database not available",
			ShowAlert: true,
		})
		return nil
	}

	// Parse rating from Extra field
	rating, err := strconv.ParseInt(cbData.Extra, 10, 64)
	if err != nil {
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Invalid rating",
			ShowAlert: true,
		})
		return nil
	}

	// Rating 0 means clear/remove rating
	var ratingPtr *int
	ratingText := "cleared"
	if rating > 0 && rating <= 10 {
		ratingInt := int(rating)
		ratingPtr = &ratingInt
		ratingText = fmt.Sprintf("set to %d/10", rating)
	}

	err = db.UpdateUserGameRating(context.Background(), cbData.UserID, cbData.GameID, ratingPtr)
	if err != nil {
		log.Printf("Error updating rating: %v", err)
		_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Error updating rating",
			ShowAlert: true,
		})
		return nil
	}
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: fmt.Sprintf("Rating %s", ratingText),
	})

	// Fetch game and show edit options again
	game, err := db.GetUserGame(context.Background(), cbData.UserID, cbData.GameID)
	if err != nil {
		return nil
	}

	return sendGameEditOptions(b, ctx, game)
}

func handleImportAllCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData, cfg *config.Config) error {
	return performSteamImport(b, ctx, cbData, cfg, false)
}

func handleImportPlayedCallback(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData, cfg *config.Config) error {
	return performSteamImport(b, ctx, cbData, cfg, true)
}

func performSteamImport(b *gotgbot.Bot, ctx *ext.Context, cbData GameTrackingCallbackData, cfg *config.Config, playedOnly bool) error {
	steamID := strconv.FormatInt(cbData.GameID, 10) // GameID field holds Steam ID for import callbacks

	// Answer callback and update message immediately
	_, _ = ctx.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Starting import..."})

	importType := "all games"
	if playedOnly {
		importType = "played games"
	}

	// Edit message to show import is in progress (removes buttons to prevent double-clicks)
	if ctx.CallbackQuery.Message != nil {
		_, _, _ = ctx.CallbackQuery.Message.EditText(b,
			fmt.Sprintf("Importing %s in background...\nThis may take a while depending on your library size.", importType),
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	}

	// Run import in background
	go func() {
		// Fetch owned games
		gamesResp, err := steam.GetSteamOwnedGames(cfg.SteamAPIKey, steamID)
		if err != nil {
			if ctx.CallbackQuery.Message != nil {
				_, _, _ = ctx.CallbackQuery.Message.EditText(b,
					"Error fetching Steam library. Please try again later.",
					&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
			}
			return
		}

		db := steam.GetDatabase()
		if db == nil {
			if ctx.CallbackQuery.Message != nil {
				_, _, _ = ctx.CallbackQuery.Message.EditText(b,
					"Database not available.",
					&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
			}
			return
		}

		games := make([]steam.UserGame, 0)
		totalProcessed := 0
		for _, ownedGame := range gamesResp.Response.Games {
			playTimeHours := float64(ownedGame.PlaytimeForever) / 60.0

			// Skip unplayed games if playedOnly filter is enabled
			if playedOnly && playTimeHours == 0 {
				continue
			}

			appIDStr := strconv.Itoa(ownedGame.AppID)

			// Determine status based on playtime
			var status steam.GameStatus
			if playTimeHours == 0 {
				status = steam.StatusNotStarted
			} else {
				// Try to get HLTB data to determine if completed
				hltbData, _ := steam.GetHltbData(context.Background(), appIDStr, ownedGame.Name)
				if hltbData.MainStory > 0 && playTimeHours >= float64(hltbData.MainStory) {
					status = steam.StatusCompleted
				} else {
					status = steam.StatusPlaying
				}
			}

			game := steam.UserGame{
				UserID:      cbData.UserID,
				AppID:       sql.NullString{String: appIDStr, Valid: true},
				GameName:    ownedGame.Name,
				Status:      status,
				TimePlayed:  playTimeHours,
				IsFavorite:  false,
				IsSteamGame: true,
			}

			games = append(games, game)

			// Process in batches to avoid overwhelming the system
			if len(games) >= 50 {
				inserted, _ := db.BulkAddUserGames(context.Background(), games)
				totalProcessed += inserted
				if ctx.CallbackQuery.Message != nil {
					_, _, _ = ctx.CallbackQuery.Message.EditText(b,
						fmt.Sprintf("Importing %s in background...\n%d games added so far.", importType, totalProcessed),
						&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
				}
				games = make([]steam.UserGame, 0)
			}
		}

		// Insert remaining games
		if len(games) > 0 {
			inserted, _ := db.BulkAddUserGames(context.Background(), games)
			totalProcessed += inserted
		}

		msg := fmt.Sprintf(
			"<b>Import Complete!</b>\n\n"+
				"Added %d games to your library.\n\n"+
				"Use /mygamestats to view your library!",
			totalProcessed,
		)

		if ctx.CallbackQuery.Message != nil {
			_, _, _ = ctx.CallbackQuery.Message.EditText(b, msg, &gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		}
	}()

	return nil
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
