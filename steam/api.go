package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/url"
	"steam_bot/utils"
	"strings"
	"sync"
	"time"

	"github.com/rshero/hltb"
)

// ----- HLTB Client Singleton -----

var (
	hltbClient     *hltb.Client
	hltbClientOnce sync.Once
	hltbClientErr  error
)

// ----- Database Package Variable -----

var db *Database

func getHltbClient() (*hltb.Client, error) {
	hltbClientOnce.Do(func() {
		hltbClient, hltbClientErr = hltb.NewClientWithInit()
		if hltbClientErr != nil {
			log.Println("Error initializing HLTB client:", hltbClientErr)
		}
	})
	return hltbClient, hltbClientErr
}

// ----- Database Access -----

func SetDatabase(database *Database) {
	db = database
}

func GetDatabase() *Database {
	return db
}

// ----- API Response Types -----

type CheapSharkDeal struct {
	Title       string `json:"title"`
	DealID      string `json:"dealID"`
	StoreID     string `json:"storeID"`
	GameID      string `json:"gameID"`
	SalePrice   string `json:"salePrice"`
	NormalPrice string `json:"normalPrice"`
	IsOnSale    string `json:"isOnSale"`
	Savings     string `json:"savings"`
	Metacritic  string `json:"metacriticScore"`
	SteamRating string `json:"steamRatingText"`
	SteamAppID  string `json:"steamAppID"`
	ReleaseDate int64  `json:"releaseDate"`
	LastChange  int64  `json:"lastChange"`
	DealRating  string `json:"dealRating"`
	Thumb       string `json:"thumb"`
}

type SteamAppDetailsResponse struct {
	Success bool            `json:"success"`
	Data    SteamAppDetails `json:"data"`
}

type Category struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
}

type Genre struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type Metacritic struct {
	Score int    `json:"score"`
	URL   string `json:"url"`
}

type ReleaseDate struct {
	ComingSoon bool   `json:"coming_soon"`
	Date       string `json:"date"`
}

type PriceOverview struct {
	FinalFormatted string `json:"final_formatted"`
	Final          int    `json:"final"`
}

type PcRequirements struct {
	Minimum     string `json:"minimum"`
	Recommended string `json:"recommended"`
}

type SteamAppDetails struct {
	Name             string          `json:"name"`
	AppType          string          `json:"type"`
	ShortDescription string          `json:"short_description"`
	IsFree           bool            `json:"is_free"`
	HeaderImage      string          `json:"header_image"`
	PriceOverview    PriceOverview   `json:"price_overview"`
	PcRequirements   json.RawMessage `json:"pc_requirements"`
	Metacritic       Metacritic      `json:"metacritic"`
	Categories       []Category      `json:"categories"`
	Genres           []Genre         `json:"genres"`
	Developers       []string        `json:"developers"`
	Publishers       []string        `json:"publishers"`
	ReleaseDate      ReleaseDate     `json:"release_date"`
}

// ----- Helper Methods for SteamAppDetails -----

// CategoryNames extracts category description strings from the details
func (d *SteamAppDetails) CategoryNames() []string {
	names := make([]string, 0, len(d.Categories))
	for _, cat := range d.Categories {
		names = append(names, cat.Description)
	}
	return names
}

// GenreNames extracts genre description strings from the details
func (d *SteamAppDetails) GenreNames() []string {
	names := make([]string, 0, len(d.Genres))
	for _, genre := range d.Genres {
		names = append(names, genre.Description)
	}
	return names
}

// FormattedPrice returns a formatted price string handling free games and edge cases
func (d *SteamAppDetails) FormattedPrice() string {
	if d.IsFree {
		return "Free"
	}
	price := d.PriceOverview.FinalFormatted
	releaseDate := d.ReleaseDate.Date

	// Price takes priority if available
	if price != "" {
		return strings.ReplaceAll(price, " ", "")
	}

	switch {
	case releaseDate == "To be announced" || releaseDate == "Coming soon":
		return releaseDate
	case d.ReleaseDate.ComingSoon:
		return "Coming soon"
	default:
		return "N/A"
	}
}

// GetPcRequirements parses and returns the PC requirements
func (d *SteamAppDetails) GetPcRequirements() PcRequirements {
	var reqs PcRequirements
	_ = json.Unmarshal(d.PcRequirements, &reqs)
	return reqs
}

// GetRequirementsWithCache fetches requirements with database caching
func GetRequirementsWithCache(ctx context.Context, appID string, details *SteamAppDetails) PcRequirements {
	if db != nil {
		if cached, err := db.GetRequirements(ctx, appID); err == nil && cached != nil {
			log.Printf("[DB] Requirements cache hit for appID %s", appID)
			return PcRequirements{
				Minimum:     cached.Minimum,
				Recommended: cached.Recommended,
			}
		}
	}

	reqs := details.GetPcRequirements()

	if db != nil {
		err := db.SetRequirements(ctx, appID, reqs.Minimum, reqs.Recommended, 30*24*time.Hour)
		if err != nil {
			log.Printf("[DB] Error caching requirements for appID %s: %v", appID, err)
		} else {
			log.Printf("[DB] Cached requirements for appID %s", appID)
		}
	}

	return reqs
}

// ----- AppInfo: Simplified result type for common use cases -----

// AppInfo contains commonly needed app information in a clean struct
type AppInfo struct {
	Description string
	HeaderImage string
	Price       string
	Categories  []string
	Genres      []string
}

// ToAppInfo converts full details to a simplified AppInfo struct
func (d *SteamAppDetails) ToAppInfo() AppInfo {
	return AppInfo{
		Description: d.ShortDescription,
		HeaderImage: d.HeaderImage,
		Price:       d.FormattedPrice(),
		Categories:  d.CategoryNames(),
		Genres:      d.GenreNames(),
	}
}

// ----- Steam Review Types -----

type SteamReviewSummaryResponse struct {
	Success      int                `json:"success"`
	QuerySummary SteamReviewSummary `json:"query_summary"`
}

type SteamReviewSummary struct {
	ReviewScoreDesc string `json:"review_score_desc"`
	TotalPositive   int    `json:"total_positive"`
	TotalNegative   int    `json:"total_negative"`
	TotalReviews    int    `json:"total_reviews"`
}

// ----- Steam Search Types -----

type SteamSearchResult struct {
	Items []SteamSearchItem `json:"items"`
}

type SteamSearchItem struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	TinyImage string `json:"tiny_image"`
	Price     struct {
		Final int `json:"final"`
	} `json:"price"`
}

// ----- Steam User Types -----

type SteamVanityURLResponse struct {
	Response struct {
		SteamID string `json:"steamid"`
		Success int    `json:"success"`
	} `json:"response"`
}

type SteamPlayerSummariesResponse struct {
	Response struct {
		Players []SteamPlayerSummary `json:"players"`
	} `json:"response"`
}

type SteamPlayerSummary struct {
	SteamID      string `json:"steamid"`
	PersonaName  string `json:"personaname"`
	ProfileURL   string `json:"profileurl"`
	Avatar       string `json:"avatarfull"`
	PersonaState int    `json:"personastate"`
	TimeCreated  int64  `json:"timecreated"`
	CountryCode  string `json:"loccountrycode"`
}

type SteamPlayerLevelResponse struct {
	Response struct {
		PlayerLevel int `json:"player_level"`
	} `json:"response"`
}

type SteamOwnedGamesResponse struct {
	Response struct {
		GameCount int              `json:"game_count"`
		Games     []SteamOwnedGame `json:"games"`
	} `json:"response"`
}

type SteamOwnedGame struct {
	AppID           int    `json:"appid"`
	Name            string `json:"name"`
	PlaytimeForever int    `json:"playtime_forever"` // in minutes
	Playtime2Weeks  int    `json:"playtime_2weeks"`  // in minutes
	ImgIconURL      string `json:"img_icon_url"`
}

type SteamUserInfo struct {
	SteamID      string
	Summary      SteamPlayerSummary
	Level        int
	GameCount    int
	GamesPlayed  int     // Games with playtime > 0
	TotalHours   float64 // Total hours across all games
	AccountValue string  // Estimated account value with currency symbol or "-" for calculating
}

// ----- Steam Profile Items Types -----

// SteamCDN is the base URL for Steam community assets
const SteamCDN = "https://shared.akamai.steamstatic.com/community_assets/images/"

type ProfileItemsResponse struct {
	Response ProfileItems `json:"response"`
}

type ProfileItems struct {
	ProfileBackground     ProfileItem `json:"profile_background"`
	MiniProfileBackground ProfileItem `json:"mini_profile_background"`
	AvatarFrame           ProfileItem `json:"avatar_frame"`
	AnimatedAvatar        ProfileItem `json:"animated_avatar"`
}

type ProfileItem struct {
	CommunityItemID string `json:"communityitemid"`
	ImageLarge      string `json:"image_large"`
	ImageSmall      string `json:"image_small"`
	Name            string `json:"name"`
	ItemTitle       string `json:"item_title"`
	AppID           int    `json:"appid"`
	MovieWebm       string `json:"movie_webm"`
	MovieMp4        string `json:"movie_mp4"`
}

// ----- API Functions -----

// GetCheapSharkDeals fetches current deals from CheapShark API
func GetCheapSharkDeals() ([]CheapSharkDeal, error) {
	apiURL := "https://www.cheapshark.com/api/1.0/deals?storeID=1&upperPrice=30&pageSize=10"

	var deals []CheapSharkDeal
	if err := utils.HttpGetJSON(apiURL, &deals); err != nil {
		return nil, fmt.Errorf("fetching deals: %w", err)
	}

	return deals, nil
}

// GetFullSteamAppDetails fetches complete app details from Steam API with caching
func GetFullSteamAppDetails(appID string) (*SteamAppDetails, error) {
	return appDetailsCache.GetOrFetch(appID, func() (*SteamAppDetails, error) {
		return FetchSteamAppDetails(appID, "")
	})
}

// FetchSteamAppDetails performs the actual API call (uncached)
// cc is the country code for pricing (e.g., "US", "in"). Defaults to "in" if empty.
func FetchSteamAppDetails(appID string, cc string) (*SteamAppDetails, error) {
	if cc == "" {
		cc = "in"
	}
	apiURL := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=%s", appID, cc)

	var response map[string]SteamAppDetailsResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return nil, fmt.Errorf("fetching app details: %w", err)
	}

	data, ok := response[appID]
	if !ok || !data.Success {
		return nil, fmt.Errorf("no details found for appID %s", appID)
	}

	return &data.Data, nil
}

// GetSteamAppInfo fetches app details and returns simplified AppInfo
// This uses the cache internally via GetFullSteamAppDetails
func GetSteamAppInfo(appID string) (AppInfo, error) {
	details, err := GetFullSteamAppDetails(appID)
	if err != nil {
		return AppInfo{Description: "No description available"}, err
	}

	return details.ToAppInfo(), nil
}

// BatchPriceResponse represents the response for multiple app price queries
type BatchPriceResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

// FetchBatchPriceOverview fetches prices for multiple apps in batches
// Returns: priceOverviews map, total price in cents, appIds without price data, error
func FetchBatchPriceOverview(appIds []int, countryCode string) (map[string]PriceOverview, int, []int, error) {
	if len(appIds) == 0 {
		return map[string]PriceOverview{}, 0, nil, nil
	}

	// Use 'in' as default country code if not provided
	if countryCode == "" {
		countryCode = "in"
	}

	// Process in batches of 50 to avoid overwhelming the API and improve response time
	const batchSize = 50
	priceOverviews := make(map[string]PriceOverview)
	foundAppIds := make(map[int]bool)
	totalPrice := 0

	// Use goroutines to fetch batches concurrently
	type batchResult struct {
		prices    map[string]PriceOverview
		found     map[int]bool
		totalCost int
		err       error
	}

	numBatches := (len(appIds) + batchSize - 1) / batchSize
	results := make(chan batchResult, numBatches)

	// Limit concurrent requests to avoid rate limiting
	semaphore := make(chan struct{}, 3)

	for i := 0; i < len(appIds); i += batchSize {
		end := min(i+batchSize, len(appIds))
		batch := appIds[i:end]

		go func(batchAppIds []int) {
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			result := batchResult{
				prices: make(map[string]PriceOverview),
				found:  make(map[int]bool),
			}

			// Convert app IDs to strings and join with commas
			appIdStrs := make([]string, len(batchAppIds))
			for j, id := range batchAppIds {
				appIdStrs[j] = fmt.Sprintf("%d", id)
			}
			appIdsParam := strings.Join(appIdStrs, ",")

			apiURL := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=%s&filters=price_overview",
				appIdsParam, countryCode)

			var response map[string]BatchPriceResponse
			if err := utils.HttpGetJSON(apiURL, &response); err != nil {
				result.err = fmt.Errorf("fetching batch price overview: %w", err)
				results <- result
				return
			}

			for appID, appData := range response {
				if !appData.Success || len(appData.Data) == 0 {
					continue
				}

				// Try to parse the data field - it might be an object or an array
				var dataObj struct {
					PriceOverview *PriceOverview `json:"price_overview"`
				}

				if err := json.Unmarshal(appData.Data, &dataObj); err != nil {
					// If unmarshal fails, data might be an array or invalid - skip this app
					continue
				}

				if dataObj.PriceOverview != nil && dataObj.PriceOverview.Final > 0 {
					// Skip if the formatted price is "Free" (temporary promotions or incorrect data)
					if dataObj.PriceOverview.FinalFormatted == "Free" {
						continue
					}
					result.prices[appID] = *dataObj.PriceOverview
					result.totalCost += dataObj.PriceOverview.Final

					// Mark this app ID as found (convert string back to int)
					var intID int
					if _, err := fmt.Sscanf(appID, "%d", &intID); err == nil {
						result.found[intID] = true
					}
				}
			}

			results <- result
		}(batch)
	}

	// Collect results from all batches
	for range numBatches {
		result := <-results
		if result.err != nil {
			log.Printf("Error fetching batch: %v", result.err)
			continue
		}

		maps.Copy(priceOverviews, result.prices)
		for appID := range result.found {
			foundAppIds[appID] = true
		}
		totalPrice += result.totalCost
	}

	// Find missing app IDs
	missingAppIds := []int{}
	for _, appID := range appIds {
		if !foundAppIds[appID] {
			missingAppIds = append(missingAppIds, appID)
		}
	}

	return priceOverviews, totalPrice, missingAppIds, nil
}

// GetSteamAppReviews fetches review summary for an app with caching
func GetSteamAppReviews(ctx context.Context, appID string) (*SteamReviewSummary, error) {
	if db != nil {
		if cached, err := db.GetReviews(ctx, appID); err == nil && cached != nil {
			log.Printf("[DB] Reviews cache hit for appID %s", appID)
			return &SteamReviewSummary{
				ReviewScoreDesc: cached.ReviewScoreDesc,
				TotalPositive:   cached.TotalPositive,
				TotalNegative:   cached.TotalNegative,
				TotalReviews:    cached.TotalReviews,
			}, nil
		}
	}

	apiURL := fmt.Sprintf("https://store.steampowered.com/appreviews/%s?json=1&num_per_page=0", appID)

	var response SteamReviewSummaryResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return nil, fmt.Errorf("fetching reviews: %w", err)
	}

	if response.Success != 1 {
		return nil, fmt.Errorf("reviews unavailable for appID %s", appID)
	}

	if db != nil {
		reviews := &response.QuerySummary
		err := db.SetReviews(ctx, appID, reviews, 6*time.Hour)
		if err != nil {
			log.Printf("[DB] Error caching reviews for appID %s: %v", appID, err)
		} else {
			log.Printf("[DB] Cached reviews for appID %s", appID)
		}
	}

	return &response.QuerySummary, nil
}

// SearchSteam searches the Steam store and returns up to 5 results
func SearchSteam(query string) ([]SteamSearchItem, error) {
	encodedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://store.steampowered.com/api/storesearch/?term=%s&l=english&cc=US", encodedQuery)

	var result SteamSearchResult
	if err := utils.HttpGetJSON(apiURL, &result); err != nil {
		return nil, fmt.Errorf("searching steam: %w", err)
	}

	const maxResults = 5
	if len(result.Items) > maxResults {
		return result.Items[:maxResults], nil
	}

	return result.Items, nil
}

// GetHltbData fetches How Long To Beat data for a game with caching
func GetHltbData(ctx context.Context, appID string, searchTerm string) (*hltb.Game, error) {
	if db != nil {
		if cached, err := db.GetHLTB(ctx, appID); err == nil && cached != nil {
			log.Printf("[DB] HLTB cache hit for appID %s", appID)
			return &hltb.Game{
				MainStory:     float32(cached.MainStory),
				MainPlusExtra: float32(cached.MainExtra),
				Completionist: float32(cached.Completionist),
				Platforms:     cached.Platforms,
			}, nil
		}
	}

	client, err := getHltbClient()
	if err != nil {
		return &hltb.Game{}, fmt.Errorf("hltb client error: %w", err)
	}

	game, err := client.SearchFirstWithDetails(searchTerm)
	if err != nil {
		return &hltb.Game{}, fmt.Errorf("hltb search error: %w", err)
	}

	if db != nil {
		err = db.SetHLTB(ctx, appID, searchTerm, float64(game.MainStory), float64(game.MainPlusExtra), float64(game.Completionist), game.Platforms)
		if err != nil {
			log.Printf("[DB] Error caching HLTB data for appID %s: %v", appID, err)
		} else {
			log.Printf("[DB] Cached HLTB data for appID %s (game: %s)", appID, searchTerm)
		}
	}

	return game, nil
}

// ----- Steam User API Functions -----

// ResolveSteamVanityURL resolves a Steam vanity URL to a Steam ID
func ResolveSteamVanityURL(apiKey, vanityURL string) (string, error) {
	apiURL := fmt.Sprintf("https://api.steampowered.com/ISteamUser/ResolveVanityURL/v0001/?key=%s&vanityurl=%s",
		apiKey, url.QueryEscape(vanityURL))

	var response SteamVanityURLResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return "", fmt.Errorf("resolving vanity URL: %w", err)
	}

	if response.Response.Success != 1 {
		return "", fmt.Errorf("user not found: %s", vanityURL)
	}

	return response.Response.SteamID, nil
}

// GetSteamPlayerSummary fetches player summary for a Steam ID
func GetSteamPlayerSummary(apiKey, steamID string) (*SteamPlayerSummary, error) {
	apiURL := fmt.Sprintf("https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v0002/?key=%s&steamids=%s",
		apiKey, steamID)

	var response SteamPlayerSummariesResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return nil, fmt.Errorf("fetching player summary: %w", err)
	}

	if len(response.Response.Players) == 0 {
		return nil, fmt.Errorf("no player found for steamID: %s", steamID)
	}

	return &response.Response.Players[0], nil
}

// GetSteamLevel fetches the Steam level for a player
func GetSteamLevel(apiKey, steamID string) (int, error) {
	apiURL := fmt.Sprintf("https://api.steampowered.com/IPlayerService/GetSteamLevel/v1/?key=%s&steamid=%s",
		apiKey, steamID)

	var response SteamPlayerLevelResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return 0, fmt.Errorf("fetching steam level: %w", err)
	}

	return response.Response.PlayerLevel, nil
}

// GetSteamOwnedGamesCount fetches the number of games owned by a player
func GetSteamOwnedGamesCount(apiKey, steamID string) (int, error) {
	response, err := GetSteamOwnedGames(apiKey, steamID)
	if err != nil {
		return 0, err
	}
	return response.Response.GameCount, nil
}

// GetSteamOwnedGames fetches detailed owned games data including playtime
func GetSteamOwnedGames(apiKey, steamID string) (*SteamOwnedGamesResponse, error) {
	apiURL := fmt.Sprintf("https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_played_free_games=true&include_appinfo=true",
		apiKey, steamID)

	var response SteamOwnedGamesResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return nil, fmt.Errorf("fetching owned games: %w", err)
	}

	return &response, nil
}

// CalculateGameStats calculates games played and total hours from owned games
func CalculateGameStats(games []SteamOwnedGame) (gamesPlayed int, totalHours float64) {
	totalMinutes := 0
	for _, game := range games {
		if game.PlaytimeForever > 0 {
			gamesPlayed++
			totalMinutes += game.PlaytimeForever
		}
	}
	totalHours = float64(totalMinutes) / 60.0
	return
}

// GetSteamUserInfo fetches complete user info by username (vanity URL)
func GetSteamUserInfo(apiKey, username string) (*SteamUserInfo, error) {
	steamID, err := ResolveSteamVanityURL(apiKey, username)
	if err != nil {
		return nil, err
	}

	summary, err := GetSteamPlayerSummary(apiKey, steamID)
	if err != nil {
		return nil, err
	}

	level, _ := GetSteamLevel(apiKey, steamID)

	// Fetch detailed games data
	gamesResponse, err := GetSteamOwnedGames(apiKey, steamID)
	gameCount := 0
	gamesPlayed := 0
	totalHours := 0.0
	accountValue := "-"

	if err == nil {
		gameCount = gamesResponse.Response.GameCount
		gamesPlayed, totalHours = CalculateGameStats(gamesResponse.Response.Games)

		// Calculate account value
		appIds := make([]int, len(gamesResponse.Response.Games))
		for i, game := range gamesResponse.Response.Games {
			appIds[i] = game.AppID
		}

		// Get country code from summary, default to "in"
		countryCode := summary.CountryCode
		if countryCode == "" {
			countryCode = "in"
		}

		_, totalPrice, _, priceErr := FetchBatchPriceOverview(appIds, countryCode)
		if priceErr == nil {
			currencySymbol := GetCurrencySymbol(countryCode)
			totalPriceFormatted := float64(totalPrice) / 100.0
			accountValue = fmt.Sprintf("%s%.0f", currencySymbol, totalPriceFormatted)
		}
	}

	return &SteamUserInfo{
		SteamID:      steamID,
		Summary:      *summary,
		Level:        level,
		GameCount:    gameCount,
		GamesPlayed:  gamesPlayed,
		TotalHours:   totalHours,
		AccountValue: accountValue,
	}, nil
}

// GetProfileCardData fetches and caches profile card data for a username
// When skipAccountValue is true, account value will be set to "-" and can be calculated later
func GetProfileCardData(ctx context.Context, apiKey, username string, skipAccountValue bool) (*SteamUserInfo, *ProfileItems, string, string, error) {
	if db != nil {
		if cached, err := db.GetProfileCard(ctx, username); err == nil && cached != nil {
			log.Printf("[DB] Profile card cache hit for username %s (SteamID from cache: %s)", username, cached.SteamID)

			info := &SteamUserInfo{
				SteamID: cached.SteamID,
				Summary: SteamPlayerSummary{
					SteamID:      cached.SteamID,
					PersonaName:  cached.PersonaName,
					ProfileURL:   fmt.Sprintf("https://steamcommunity.com/profiles/%s", cached.SteamID),
					Avatar:       cached.Avatar,
					CountryCode:  cached.CountryCode,
					PersonaState: personaStateToInt(cached.Status),
				},
				Level:        cached.Level,
				GameCount:    cached.GameCount,
				GamesPlayed:  cached.GamesPlayed,
				TotalHours:   cached.TotalHours,
				AccountValue: cached.AccountValue,
			}

			items := &ProfileItems{}
			if cached.Frame != "" {
				frameURL := strings.TrimPrefix(cached.Frame, SteamCDN)
				items.AvatarFrame = ProfileItem{ImageLarge: frameURL}
			}
			if cached.BackgroundURL != "" {
				bgURL := strings.TrimPrefix(cached.BackgroundURL, SteamCDN)
				items.ProfileBackground = ProfileItem{ImageLarge: bgURL}
			}

			return info, items, cached.Status, cached.ImageURL, nil
		}
	}

	// Check if username is already a numeric SteamID64 (17 digits starting with 7656119)
	// If so, skip vanity URL resolution
	var steamID string
	if len(username) == 17 && strings.HasPrefix(username, "7656119") {
		// It's already a SteamID64, use it directly
		steamID = username
		log.Printf("Input is already a SteamID64: %s", steamID)
	} else {
		// It's a vanity URL/username, resolve it
		var err error
		steamID, err = ResolveSteamVanityURL(apiKey, username)
		if err != nil {
			return nil, nil, "", "", err
		}
	}

	summary, err := GetSteamPlayerSummary(apiKey, steamID)
	if err != nil {
		return nil, nil, "", "", err
	}

	level, _ := GetSteamLevel(apiKey, steamID)

	gamesResponse, err := GetSteamOwnedGames(apiKey, steamID)
	gameCount := 0
	gamesPlayed := 0
	totalHours := 0.0
	accountValue := "-"

	if err == nil {
		gameCount = gamesResponse.Response.GameCount
		gamesPlayed, totalHours = CalculateGameStats(gamesResponse.Response.Games)

		// Only calculate account value if not skipping
		if !skipAccountValue {
			// Calculate account value
			appIds := make([]int, len(gamesResponse.Response.Games))
			for i, game := range gamesResponse.Response.Games {
				appIds[i] = game.AppID
			}

			// Get country code from summary, default to "in"
			countryCode := summary.CountryCode
			if countryCode == "" {
				countryCode = "in"
			}

			_, totalPrice, _, priceErr := FetchBatchPriceOverview(appIds, countryCode)
			if priceErr == nil {
				currencySymbol := GetCurrencySymbol(countryCode)
				totalPriceFormatted := float64(totalPrice) / 100.0
				accountValue = fmt.Sprintf("%s%.0f", currencySymbol, totalPriceFormatted)
			}
		}
	}

	info := &SteamUserInfo{
		SteamID:      steamID,
		Summary:      *summary,
		Level:        level,
		GameCount:    gameCount,
		GamesPlayed:  gamesPlayed,
		TotalHours:   totalHours,
		AccountValue: accountValue,
	}

	profileItems, _ := GetProfileItemsEquipped(apiKey, steamID)
	status := personaStateToString(summary.PersonaState)

	// Note: imageURL will be empty here and updated later by the caller after image upload
	return info, profileItems, status, "", nil
}

// CalculateAccountValueAsync calculates the account value asynchronously
// and calls the callback function with the updated SteamUserInfo when done
func CalculateAccountValueAsync(apiKey, steamID string, info *SteamUserInfo, callback func(*SteamUserInfo)) {
	go func() {
		gamesResponse, err := GetSteamOwnedGames(apiKey, steamID)
		if err != nil {
			log.Printf("Error fetching games for account value calculation: %v", err)
			return
		}

		appIds := make([]int, len(gamesResponse.Response.Games))
		for i, game := range gamesResponse.Response.Games {
			appIds[i] = game.AppID
		}

		// Get country code from summary, default to "in"
		countryCode := info.Summary.CountryCode
		if countryCode == "" {
			countryCode = "in"
		}

		_, totalPrice, _, priceErr := FetchBatchPriceOverview(appIds, countryCode)
		if priceErr == nil {
			currencySymbol := GetCurrencySymbol(countryCode)
			totalPriceFormatted := float64(totalPrice) / 100.0
			info.AccountValue = fmt.Sprintf("%s%.0f", currencySymbol, totalPriceFormatted)
			callback(info)
		} else {
			log.Printf("Error calculating account value: %v", priceErr)
		}
	}()
}

// UpdateProfileCardImage updates the cached image URL for a profile card
func UpdateProfileCardImage(ctx context.Context, username, steamID string, info *SteamUserInfo, profileItems *ProfileItems, status, imageURL string) error {
	if db == nil {
		return nil
	}
	return db.SetProfileCard(ctx, username, steamID, info, profileItems, status, imageURL, 1*time.Hour)
}

// GetCurrencySymbol returns the currency symbol based on country code
func GetCurrencySymbol(countryCode string) string {
	// Normalize to uppercase for consistent lookup
	countryCode = strings.ToUpper(countryCode)

	currencyMap := map[string]string{
		"US": "$",    // United States Dollar
		"GB": "£",    // British Pound
		"EU": "€",    // Euro (used for many EU countries)
		"MC": "€",    // Monaco Euro
		"DE": "€",    // Germany
		"FR": "€",    // France
		"IT": "€",    // Italy
		"ES": "€",    // Spain
		"NL": "€",    // Netherlands
		"BE": "€",    // Belgium
		"AT": "€",    // Austria
		"PT": "€",    // Portugal
		"IE": "€",    // Ireland
		"FI": "€",    // Finland
		"GR": "€",    // Greece
		"JP": "¥",    // Japanese Yen
		"CN": "¥",    // Chinese Yuan
		"KR": "₩",    // Korean Won
		"IN": "Rs. ", // Indian Rupee (using ASCII-safe notation for font compatibility)
		"RU": "₽",    // Russian Ruble
		"BR": "R$",   // Brazilian Real
		"MX": "$",    // Mexican Peso
		"AU": "A$",   // Australian Dollar
		"CA": "C$",   // Canadian Dollar
		"CH": "CHF",  // Swiss Franc
		"SE": "kr",   // Swedish Krona
		"NO": "kr",   // Norwegian Krone
		"DK": "kr",   // Danish Krone
		"PL": "zł",   // Polish Zloty
		"TR": "₺",    // Turkish Lira
		"ZA": "R",    // South African Rand
		"AR": "$",    // Argentine Peso
		"CL": "$",    // Chilean Peso
		"CO": "$",    // Colombian Peso
		"PE": "S/",   // Peruvian Sol
		"HK": "HK$",  // Hong Kong Dollar
		"TW": "NT$",  // Taiwan Dollar
		"SG": "S$",   // Singapore Dollar
		"MY": "RM",   // Malaysian Ringgit
		"TH": "฿",    // Thai Baht
		"ID": "Rp",   // Indonesian Rupiah
		"PH": "₱",    // Philippine Peso
		"VN": "₫",    // Vietnamese Dong
		"NZ": "NZ$",  // New Zealand Dollar
		"IL": "₪",    // Israeli Shekel
		"SA": "SR",   // Saudi Riyal
		"AE": "AED",  // UAE Dirham
		"KW": "KD",   // Kuwaiti Dinar
		"QA": "QR",   // Qatari Riyal
		"CZ": "Kč",   // Czech Koruna
		"HU": "Ft",   // Hungarian Forint
		"RO": "lei",  // Romanian Leu
		"BG": "лв",   // Bulgarian Lev
		"HR": "kn",   // Croatian Kuna
		"UA": "₴",    // Ukrainian Hryvnia
		"KZ": "₸",    // Kazakhstani Tenge
	}

	if symbol, ok := currencyMap[countryCode]; ok {
		return symbol
	}
	return "Rs. " // Default to INR (ASCII-safe)
}

func personaStateToInt(status string) int {
	switch status {
	case "Online":
		return 1
	case "Busy":
		return 2
	case "Away":
		return 3
	case "Snooze":
		return 4
	case "Looking to trade":
		return 5
	case "Looking to play":
		return 6
	default:
		return 0
	}
}

// GetProfileItemsEquipped fetches the equipped profile items (background, frame, etc.) for a user
func GetProfileItemsEquipped(apiKey, steamID string) (*ProfileItems, error) {
	apiURL := fmt.Sprintf("https://api.steampowered.com/IPlayerService/GetProfileItemsEquipped/v1/?steamid=%s&key=%s",
		steamID, apiKey)

	var response ProfileItemsResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return nil, fmt.Errorf("fetching profile items: %w", err)
	}

	return &response.Response, nil
}

// personaStateToString converts Steam persona state to human readable string
func personaStateToString(state int) string {
	switch state {
	case 0:
		return "Offline"
	case 1:
		return "Online"
	case 2:
		return "Busy"
	case 3:
		return "Away"
	case 4:
		return "Snooze"
	case 5:
		return "Looking to trade"
	case 6:
		return "Looking to play"
	default:
		return "Unknown"
	}
}
