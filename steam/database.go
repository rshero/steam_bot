package steam

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
}

func InitDatabase(ctx context.Context, dbPath string) (*Database, error) {
	sqliteDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_timeout=5000")
	if err != nil {
		return nil, err
	}

	database := &Database{db: sqliteDB}

	if err := database.createTables(ctx); err != nil {
		return nil, err
	}

	go database.periodicCleanup()

	return database, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) createTables(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS profile_cards_cache (
			username TEXT PRIMARY KEY,
			steam_id TEXT UNIQUE,
			avatar TEXT,
			frame TEXT,
			persona_name TEXT,
			level INTEGER,
			country_code TEXT,
			game_count INTEGER,
			games_played INTEGER,
			total_hours REAL,
			account_value TEXT,
			status TEXT,
			background_url TEXT,
			image_url TEXT,
			cached_at INTEGER,
			expires_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_username ON profile_cards_cache(username)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_expires ON profile_cards_cache(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_steamid ON profile_cards_cache(steam_id)`,

		`CREATE TABLE IF NOT EXISTS hltb_cache (
			app_id TEXT PRIMARY KEY,
			game_name TEXT,
			main_story REAL,
			main_extra REAL,
			completionist REAL,
			platforms TEXT,
			cached_at INTEGER,
			expires_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hltb_appid ON hltb_cache(app_id)`,
		`CREATE INDEX IF NOT EXISTS idx_hltb_expires ON hltb_cache(expires_at)`,

		`CREATE TABLE IF NOT EXISTS requirements_cache (
			app_id TEXT PRIMARY KEY,
			minimum TEXT,
			recommended TEXT,
			cached_at INTEGER,
			expires_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_req_appid ON requirements_cache(app_id)`,
		`CREATE INDEX IF NOT EXISTS idx_req_expires ON requirements_cache(expires_at)`,

		`CREATE TABLE IF NOT EXISTS reviews_cache (
			app_id TEXT PRIMARY KEY,
			review_score_desc TEXT,
			total_positive INTEGER,
			total_negative INTEGER,
			total_reviews INTEGER,
			cached_at INTEGER,
			expires_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reviews_appid ON reviews_cache(app_id)`,
		`CREATE INDEX IF NOT EXISTS idx_reviews_expires ON reviews_cache(expires_at)`,

		`CREATE TABLE IF NOT EXISTS sent_posts (
			deal_id TEXT PRIMARY KEY,
			sent_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sent_posts_dealid ON sent_posts(deal_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sent_posts_sentat ON sent_posts(sent_at)`,
	}

	for _, query := range queries {
		if _, err := d.db.ExecContext(ctx, query); err != nil {
			return err
		}
	}

	// Migrate existing tables
	_, _ = d.db.ExecContext(ctx, `ALTER TABLE profile_cards_cache ADD COLUMN image_url TEXT`)

	// Note: SQLite doesn't support changing column types directly
	// For account_value INTEGER -> TEXT migration, we'll just let old rows fail gracefully
	// and new inserts will work correctly. Users can clear cache to fix.

	return nil
}

func (d *Database) periodicCleanup() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	d.CleanupExpired(context.Background())

	for range ticker.C {
		d.CleanupExpired(context.Background())
	}
}

func (d *Database) CleanupExpired(ctx context.Context) error {
	now := time.Now().Unix()

	tables := []string{
		"DELETE FROM profile_cards_cache WHERE expires_at < ?",
		"DELETE FROM hltb_cache WHERE expires_at < ?",
		"DELETE FROM requirements_cache WHERE expires_at < ?",
		"DELETE FROM reviews_cache WHERE expires_at < ?",
	}

	for _, query := range tables {
		result, err := d.db.ExecContext(ctx, query, now)
		if err != nil {
			log.Printf("Error cleaning expired entries: %v", err)
			continue
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected > 0 {
			log.Printf("Cleaned up %d expired entries", rowsAffected)
		}
	}

	return nil
}

type ProfileCardCache struct {
	Username      string
	SteamID       string
	Avatar        string
	Frame         string
	PersonaName   string
	Level         int
	CountryCode   string
	GameCount     int
	GamesPlayed   int
	TotalHours    float64
	AccountValue  string
	Status        string
	BackgroundURL string
	ImageURL      string
	CachedAt      int64
	ExpiresAt     int64
}

func (d *Database) ClearProfileCardCache(ctx context.Context, username string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM profile_cards_cache WHERE username = ?", username)
	return err
}

func (d *Database) ClearProfileCardCacheBySteamID(ctx context.Context, steamID string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM profile_cards_cache WHERE steam_id = ?", steamID)
	return err
}

func (d *Database) GetProfileCard(ctx context.Context, username string) (*ProfileCardCache, error) {
	query := `SELECT username, steam_id, avatar, frame, persona_name, level, country_code,
			  game_count, games_played, total_hours, account_value, status, background_url,
			  image_url, cached_at, expires_at
			  FROM profile_cards_cache WHERE username = ? AND expires_at > ?`

	row := d.db.QueryRowContext(ctx, query, username, time.Now().Unix())

	var cache ProfileCardCache
	err := row.Scan(
		&cache.Username, &cache.SteamID, &cache.Avatar, &cache.Frame,
		&cache.PersonaName, &cache.Level, &cache.CountryCode,
		&cache.GameCount, &cache.GamesPlayed, &cache.TotalHours,
		&cache.AccountValue, &cache.Status, &cache.BackgroundURL,
		&cache.ImageURL, &cache.CachedAt, &cache.ExpiresAt,
	)

	if err != nil {
		return nil, err
	}

	return &cache, nil
}

func (d *Database) SetProfileCard(ctx context.Context, username, steamID string, info *SteamUserInfo, profileItems *ProfileItems, status, imageURL string, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl).Unix()
	cachedAt := now.Unix()

	backgroundURL := ""
	frameURL := ""

	if profileItems != nil {
		if profileItems.ProfileBackground.ImageLarge != "" {
			backgroundURL = SteamCDN + profileItems.ProfileBackground.ImageLarge
		}
		if profileItems.AvatarFrame.ImageLarge != "" {
			frameURL = SteamCDN + profileItems.AvatarFrame.ImageLarge
		}
	}

	query := `INSERT OR REPLACE INTO profile_cards_cache
			  (username, steam_id, avatar, frame, persona_name, level, country_code,
			   game_count, games_played, total_hours, account_value, status, background_url,
			   image_url, cached_at, expires_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := d.db.ExecContext(ctx, query,
		username, steamID, info.Summary.Avatar, frameURL,
		info.Summary.PersonaName, info.Level, info.Summary.CountryCode,
		info.GameCount, info.GamesPlayed, info.TotalHours,
		info.AccountValue, status, backgroundURL, imageURL,
		cachedAt, expiresAt,
	)

	return err
}

type HLTBCache struct {
	AppID         string
	GameName      string
	MainStory     float64
	MainExtra     float64
	Completionist float64
	Platforms     []string
	CachedAt      int64
	ExpiresAt     int64
}

func (d *Database) GetHLTB(ctx context.Context, appID string) (*HLTBCache, error) {
	query := `SELECT app_id, game_name, main_story, main_extra, completionist,
			  platforms, cached_at, expires_at
			  FROM hltb_cache WHERE app_id = ? AND expires_at > ?`

	row := d.db.QueryRowContext(ctx, query, appID, time.Now().Unix())

	var cache HLTBCache
	var platformsJSON string

	err := row.Scan(
		&cache.AppID, &cache.GameName, &cache.MainStory,
		&cache.MainExtra, &cache.Completionist, &platformsJSON,
		&cache.CachedAt, &cache.ExpiresAt,
	)

	if err != nil {
		return nil, err
	}

	if platformsJSON != "" {
		if err := json.Unmarshal([]byte(platformsJSON), &cache.Platforms); err != nil {
			log.Printf("Error unmarshaling platforms JSON for appID %s: %v", appID, err)
		}
	}

	return &cache, nil
}

func (d *Database) SetHLTB(ctx context.Context, appID, gameName string, mainStory, mainExtra, completionist float64, platforms []string, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl).Unix()
	cachedAt := now.Unix()

	platformsJSON, _ := json.Marshal(platforms)

	query := `INSERT OR REPLACE INTO hltb_cache
			  (app_id, game_name, main_story, main_extra, completionist, platforms, cached_at, expires_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := d.db.ExecContext(ctx, query, appID, gameName, mainStory, mainExtra, completionist, platformsJSON, cachedAt, expiresAt)
	return err
}

type RequirementsCache struct {
	AppID       string
	Minimum     string
	Recommended string
	CachedAt    int64
	ExpiresAt   int64
}

func (d *Database) GetRequirements(ctx context.Context, appID string) (*RequirementsCache, error) {
	query := `SELECT app_id, minimum, recommended, cached_at, expires_at
			  FROM requirements_cache WHERE app_id = ? AND expires_at > ?`

	row := d.db.QueryRowContext(ctx, query, appID, time.Now().Unix())

	var cache RequirementsCache
	err := row.Scan(&cache.AppID, &cache.Minimum, &cache.Recommended, &cache.CachedAt, &cache.ExpiresAt)

	if err != nil {
		return nil, err
	}

	return &cache, nil
}

func (d *Database) SetRequirements(ctx context.Context, appID, minimum, recommended string, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl).Unix()
	cachedAt := now.Unix()

	query := `INSERT OR REPLACE INTO requirements_cache
			  (app_id, minimum, recommended, cached_at, expires_at)
			  VALUES (?, ?, ?, ?, ?)`

	_, err := d.db.ExecContext(ctx, query, appID, minimum, recommended, cachedAt, expiresAt)
	return err
}

type ReviewsCache struct {
	AppID           string
	ReviewScoreDesc string
	TotalPositive   int
	TotalNegative   int
	TotalReviews    int
	CachedAt        int64
	ExpiresAt       int64
}

func (d *Database) GetReviews(ctx context.Context, appID string) (*ReviewsCache, error) {
	query := `SELECT app_id, review_score_desc, total_positive, total_negative, total_reviews, cached_at, expires_at
			  FROM reviews_cache WHERE app_id = ? AND expires_at > ?`

	row := d.db.QueryRowContext(ctx, query, appID, time.Now().Unix())

	var cache ReviewsCache
	err := row.Scan(&cache.AppID, &cache.ReviewScoreDesc, &cache.TotalPositive, &cache.TotalNegative, &cache.TotalReviews, &cache.CachedAt, &cache.ExpiresAt)

	if err != nil {
		return nil, err
	}

	return &cache, nil
}

func (d *Database) SetReviews(ctx context.Context, appID string, reviews *SteamReviewSummary, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl).Unix()
	cachedAt := now.Unix()

	query := `INSERT OR REPLACE INTO reviews_cache
			  (app_id, review_score_desc, total_positive, total_negative, total_reviews, cached_at, expires_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := d.db.ExecContext(ctx, query, appID, reviews.ReviewScoreDesc, reviews.TotalPositive, reviews.TotalNegative, reviews.TotalReviews, cachedAt, expiresAt)
	return err
}

type SentPost struct {
	DealID string
	SentAt int64
}

func (d *Database) IsSentPost(ctx context.Context, dealID string) (bool, error) {
	var exists int
	err := d.db.QueryRowContext(ctx, "SELECT 1 FROM sent_posts WHERE deal_id = ?", dealID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return exists == 1, err
}

func (d *Database) MarkSentPost(ctx context.Context, dealID string) error {
	now := time.Now().Unix()
	_, err := d.db.ExecContext(ctx, "INSERT OR IGNORE INTO sent_posts (deal_id, sent_at) VALUES (?, ?)", dealID, now)
	return err
}

func (d *Database) InitializeSentPostsFromMap(ctx context.Context, postsMap map[string]time.Time) error {
	if len(postsMap) == 0 {
		return nil
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO sent_posts (deal_id, sent_at) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for dealID, timestamp := range postsMap {
		_, err = stmt.ExecContext(ctx, dealID, timestamp.Unix())
		if err != nil {
			log.Printf("Error inserting sent post %s: %v", dealID, err)
		}
	}

	return tx.Commit()
}
