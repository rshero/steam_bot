package steam

import (
	"encoding/json"
	"fmt"
	"net/url"
	"steam_bot/utils"
	"strings"
)

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

type SteamAppDetails struct {
	Name             string          `json:"name"`
	ShortDescription string          `json:"short_description"`
	HeaderImage      string          `json:"header_image"`
	PriceOverview    PriceOverview   `json:"price_overview"`
	PcRequirements   json.RawMessage `json:"pc_requirements"`
}

type PcRequirements struct {
	Minimum     string `json:"minimum"`
	Recommended string `json:"recommended"`
}

type PriceOverview struct {
	FinalFormatted string `json:"final_formatted"`
}

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

func GetCheapSharkDeals() ([]CheapSharkDeal, error) {
	url := "https://www.cheapshark.com/api/1.0/deals?storeID=1&upperPrice=30&pageSize=10"
	var deals []CheapSharkDeal
	err := utils.HttpGetJSON(url, &deals)
	if err != nil {
		return nil, err
	}
	return deals, nil
}

func GetSteamAppDetails(appID string) (string, string, string, error) {
	details, err := GetFullSteamAppDetails(appID)
	if err != nil {
		return "No description available", "", "", err
	}

	desc := details.ShortDescription
	imageURL := details.HeaderImage
	price := details.PriceOverview.FinalFormatted
	if price == "" {
		price = "N/A"
	}
	price = strings.ReplaceAll(price, " ", "")

	return desc, imageURL, price, nil
}

func GetFullSteamAppDetails(appID string) (*SteamAppDetails, error) {
	url := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=in", appID)

	var response map[string]SteamAppDetailsResponse
	err := utils.HttpGetJSON(url, &response)
	if err != nil {
		return nil, err
	}

	data, ok := response[appID]
	if !ok || !data.Success {
		return nil, fmt.Errorf("failed to get details for appID %s", appID)
	}
	return &data.Data, nil
}

func GetSteamAppReviews(appID string) (*SteamReviewSummary, error) {
	url := fmt.Sprintf("https://store.steampowered.com/appreviews/%s?json=1&num_per_page=0", appID)
	var response SteamReviewSummaryResponse
	err := utils.HttpGetJSON(url, &response)
	if err != nil {
		return nil, err
	}
	if response.Success != 1 {
		return nil, fmt.Errorf("failed to get reviews for appID %s", appID)
	}
	return &response.QuerySummary, nil
}

func SearchSteam(query string) ([]SteamSearchItem, error) {
	encodedQuery := url.QueryEscape(query)
	url := fmt.Sprintf("https://store.steampowered.com/api/storesearch/?term=%s&l=english&cc=US", encodedQuery)
	var result SteamSearchResult
	err := utils.HttpGetJSON(url, &result)
	if err != nil {
		return nil, err
	}

	if len(result.Items) > 5 {
		return result.Items[:5], nil
	}
	return result.Items, nil
}
