package steam

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"steam_bot/utils"
	"strings"
	"sync"

	"github.com/rshero/hltb"
)

// ----- HLTB Client Singleton -----

var (
	hltbClient     *hltb.Client
	hltbClientOnce sync.Once
	hltbClientErr  error
)

func getHltbClient() (*hltb.Client, error) {
	hltbClientOnce.Do(func() {
		hltbClient, hltbClientErr = hltb.NewClientWithInit()
		if hltbClientErr != nil {
			log.Println("Error initializing HLTB client:", hltbClientErr)
		}
	})
	return hltbClient, hltbClientErr
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

	switch {
	case price == "" && releaseDate == "":
		return "N/A"
	case releaseDate == "To be announced" || releaseDate == "Coming soon":
		return releaseDate
	default:
		return strings.ReplaceAll(price, " ", "")
	}
}

// GetPcRequirements parses and returns the PC requirements
func (d *SteamAppDetails) GetPcRequirements() PcRequirements {
	var reqs PcRequirements
	_ = json.Unmarshal(d.PcRequirements, &reqs)
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
		GameCount int `json:"game_count"`
	} `json:"response"`
}

type SteamUserInfo struct {
	SteamID   string
	Summary   SteamPlayerSummary
	Level     int
	GameCount int
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
		return fetchSteamAppDetails(appID)
	})
}

// fetchSteamAppDetails performs the actual API call (internal, uncached)
func fetchSteamAppDetails(appID string) (*SteamAppDetails, error) {
	apiURL := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=in", appID)

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

// GetSteamAppReviews fetches review summary for an app
func GetSteamAppReviews(appID string) (*SteamReviewSummary, error) {
	apiURL := fmt.Sprintf("https://store.steampowered.com/appreviews/%s?json=1&num_per_page=0", appID)

	var response SteamReviewSummaryResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return nil, fmt.Errorf("fetching reviews: %w", err)
	}

	if response.Success != 1 {
		return nil, fmt.Errorf("reviews unavailable for appID %s", appID)
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

// GetHltbData fetches How Long To Beat data for a game
func GetHltbData(searchTerm string) (*hltb.Game, error) {
	client, err := getHltbClient()
	if err != nil {
		return &hltb.Game{}, fmt.Errorf("hltb client error: %w", err)
	}

	game, err := client.SearchFirstWithDetails(searchTerm)
	if err != nil {
		return &hltb.Game{}, fmt.Errorf("hltb search error: %w", err)
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
	apiURL := fmt.Sprintf("https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_played_free_games=true",
		apiKey, steamID)

	var response SteamOwnedGamesResponse
	if err := utils.HttpGetJSON(apiURL, &response); err != nil {
		return 0, fmt.Errorf("fetching owned games: %w", err)
	}

	return response.Response.GameCount, nil
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
	gameCount, _ := GetSteamOwnedGamesCount(apiKey, steamID)

	return &SteamUserInfo{
		SteamID:   steamID,
		Summary:   *summary,
		Level:     level,
		GameCount: gameCount,
	}, nil
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
