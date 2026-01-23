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

		// User game tracking tables
		`CREATE TABLE IF NOT EXISTS user_games (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			app_id TEXT,
			game_name TEXT NOT NULL,
			status TEXT DEFAULT 'completed',
			time_played REAL DEFAULT 0,
			is_favorite INTEGER DEFAULT 0,
			rating INTEGER,
			notes TEXT,
			header_image TEXT,
			price_usd INTEGER,
			price_inr INTEGER,
			hltb_main REAL,
			hltb_extra REAL,
			hltb_completionist REAL,
			is_steam_game INTEGER DEFAULT 1,
			added_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_games_user_appid ON user_games(user_id, app_id) WHERE app_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_user_games_user_id ON user_games(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_user_games_status ON user_games(user_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_user_games_favorite ON user_games(user_id, is_favorite)`,
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

// ==================== User Game Tracking ====================

type GameStatus string

const (
	StatusCompleted  GameStatus = "completed"
	StatusPlaying    GameStatus = "playing"
	StatusNotStarted GameStatus = "not_started"
	StatusOnHold     GameStatus = "on_hold"
	StatusDropped    GameStatus = "dropped"
	StatusPaused     GameStatus = "paused"
)

func (s GameStatus) IsValid() bool {
	switch s {
	case StatusCompleted, StatusPlaying, StatusNotStarted, StatusOnHold, StatusDropped, StatusPaused:
		return true
	}
	return false
}

func (s GameStatus) DisplayName() string {
	switch s {
	case StatusCompleted:
		return "Completed"
	case StatusPlaying:
		return "Playing"
	case StatusNotStarted:
		return "Not Started"
	case StatusOnHold:
		return "On Hold"
	case StatusDropped:
		return "Dropped"
	case StatusPaused:
		return "Paused"
	}
	return string(s)
}

func (s GameStatus) Emoji() string {
	switch s {
	case StatusCompleted:
		return "✓"
	case StatusPlaying:
		return "▸"
	case StatusNotStarted:
		return "○"
	case StatusOnHold:
		return "‖"
	case StatusDropped:
		return "×"
	case StatusPaused:
		return "◫"
	}
	return "·"
}

// IsBacklog returns true if the status is considered part of the backlog
func (s GameStatus) IsBacklog() bool {
	switch s {
	case StatusNotStarted, StatusOnHold, StatusDropped, StatusPaused:
		return true
	}
	return false
}

type UserGame struct {
	ID                int64
	UserID            int64
	AppID             sql.NullString
	GameName          string
	Status            GameStatus
	TimePlayed        float64
	IsFavorite        bool
	Rating            sql.NullInt64
	Notes             sql.NullString
	HeaderImage       sql.NullString
	PriceUSD          sql.NullInt64
	PriceINR          sql.NullInt64
	HLTBMain          sql.NullFloat64
	HLTBExtra         sql.NullFloat64
	HLTBCompletionist sql.NullFloat64
	IsSteamGame       bool
	AddedAt           int64
	UpdatedAt         int64
}

type UserGameStats struct {
	TotalGames     int
	TotalPlaytime  float64
	TotalFavorites int

	CompletedCount  int
	CompletedHours  float64
	PlayingCount    int
	PlayingHours    float64
	NotStartedCount int
	OnHoldCount     int
	DroppedCount    int
	PausedCount     int
	BacklogCount    int // sum of not_started, on_hold, dropped, paused

	TopRated []UserGame // Top 3 rated games
}

// AddUserGame adds a new game to user's library
func (d *Database) AddUserGame(ctx context.Context, game *UserGame) error {
	now := time.Now().Unix()
	game.AddedAt = now
	game.UpdatedAt = now

	query := `INSERT INTO user_games
		(user_id, app_id, game_name, status, time_played, is_favorite, rating, notes,
		 header_image, price_usd, price_inr, hltb_main, hltb_extra, hltb_completionist,
		 is_steam_game, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := d.db.ExecContext(ctx, query,
		game.UserID, game.AppID, game.GameName, game.Status, game.TimePlayed,
		game.IsFavorite, game.Rating, game.Notes, game.HeaderImage,
		game.PriceUSD, game.PriceINR, game.HLTBMain, game.HLTBExtra, game.HLTBCompletionist,
		game.IsSteamGame, game.AddedAt, game.UpdatedAt,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	game.ID = id
	return nil
}

// GetUserGame gets a single game by ID for a user
func (d *Database) GetUserGame(ctx context.Context, userID, gameID int64) (*UserGame, error) {
	query := `SELECT id, user_id, app_id, game_name, status, time_played, is_favorite,
		rating, notes, header_image, price_usd, price_inr, hltb_main, hltb_extra,
		hltb_completionist, is_steam_game, added_at, updated_at
		FROM user_games WHERE id = ? AND user_id = ?`

	row := d.db.QueryRowContext(ctx, query, gameID, userID)
	return scanUserGame(row)
}

// GetUserGameByAppID gets a game by Steam app ID for a user
func (d *Database) GetUserGameByAppID(ctx context.Context, userID int64, appID string) (*UserGame, error) {
	query := `SELECT id, user_id, app_id, game_name, status, time_played, is_favorite,
		rating, notes, header_image, price_usd, price_inr, hltb_main, hltb_extra,
		hltb_completionist, is_steam_game, added_at, updated_at
		FROM user_games WHERE user_id = ? AND app_id = ?`

	row := d.db.QueryRowContext(ctx, query, userID, appID)
	return scanUserGame(row)
}

// GetUserGameByName gets a game by name for a user (case-insensitive)
func (d *Database) GetUserGameByName(ctx context.Context, userID int64, gameName string) (*UserGame, error) {
	query := `SELECT id, user_id, app_id, game_name, status, time_played, is_favorite,
		rating, notes, header_image, price_usd, price_inr, hltb_main, hltb_extra,
		hltb_completionist, is_steam_game, added_at, updated_at
		FROM user_games WHERE user_id = ? AND LOWER(game_name) = LOWER(?)`

	row := d.db.QueryRowContext(ctx, query, userID, gameName)
	return scanUserGame(row)
}

// UserGameExists checks if a game already exists in user's library
func (d *Database) UserGameExists(ctx context.Context, userID int64, appID string) (bool, error) {
	var exists int
	err := d.db.QueryRowContext(ctx,
		"SELECT 1 FROM user_games WHERE user_id = ? AND app_id = ?",
		userID, appID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return exists == 1, err
}

// UserGameExistsByName checks if a game already exists by name
func (d *Database) UserGameExistsByName(ctx context.Context, userID int64, gameName string) (bool, error) {
	var exists int
	err := d.db.QueryRowContext(ctx,
		"SELECT 1 FROM user_games WHERE user_id = ? AND LOWER(game_name) = LOWER(?)",
		userID, gameName).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return exists == 1, err
}

func scanUserGame(row *sql.Row) (*UserGame, error) {
	var game UserGame
	var isFav, isSteam int

	err := row.Scan(
		&game.ID, &game.UserID, &game.AppID, &game.GameName, &game.Status,
		&game.TimePlayed, &isFav, &game.Rating, &game.Notes,
		&game.HeaderImage, &game.PriceUSD, &game.PriceINR,
		&game.HLTBMain, &game.HLTBExtra, &game.HLTBCompletionist,
		&isSteam, &game.AddedAt, &game.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	game.IsFavorite = isFav == 1
	game.IsSteamGame = isSteam == 1
	return &game, nil
}

func scanUserGames(rows *sql.Rows) ([]UserGame, error) {
	var games []UserGame
	for rows.Next() {
		var game UserGame
		var isFav, isSteam int

		err := rows.Scan(
			&game.ID, &game.UserID, &game.AppID, &game.GameName, &game.Status,
			&game.TimePlayed, &isFav, &game.Rating, &game.Notes,
			&game.HeaderImage, &game.PriceUSD, &game.PriceINR,
			&game.HLTBMain, &game.HLTBExtra, &game.HLTBCompletionist,
			&isSteam, &game.AddedAt, &game.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		game.IsFavorite = isFav == 1
		game.IsSteamGame = isSteam == 1
		games = append(games, game)
	}
	return games, rows.Err()
}

// GetUserGames retrieves games for a user with optional filters
func (d *Database) GetUserGames(ctx context.Context, userID int64, status *GameStatus, favoritesOnly bool, limit, offset int) ([]UserGame, error) {
	query := `SELECT id, user_id, app_id, game_name, status, time_played, is_favorite,
		rating, notes, header_image, price_usd, price_inr, hltb_main, hltb_extra,
		hltb_completionist, is_steam_game, added_at, updated_at
		FROM user_games WHERE user_id = ?`

	args := []interface{}{userID}

	if status != nil {
		query += " AND status = ?"
		args = append(args, *status)
	}

	if favoritesOnly {
		query += " AND is_favorite = 1"
	}

	query += " ORDER BY updated_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanUserGames(rows)
}

// GetUserBacklogGames gets all backlog games (not_started, on_hold, dropped, paused)
func (d *Database) GetUserBacklogGames(ctx context.Context, userID int64, limit, offset int) ([]UserGame, error) {
	query := `SELECT id, user_id, app_id, game_name, status, time_played, is_favorite,
		rating, notes, header_image, price_usd, price_inr, hltb_main, hltb_extra,
		hltb_completionist, is_steam_game, added_at, updated_at
		FROM user_games WHERE user_id = ? AND status IN (?, ?, ?, ?)
		ORDER BY updated_at DESC`

	args := []interface{}{userID, StatusNotStarted, StatusOnHold, StatusDropped, StatusPaused}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanUserGames(rows)
}

// UpdateUserGame updates a game in user's library
func (d *Database) UpdateUserGame(ctx context.Context, game *UserGame) error {
	game.UpdatedAt = time.Now().Unix()

	query := `UPDATE user_games SET
		game_name = ?, status = ?, time_played = ?, is_favorite = ?,
		rating = ?, notes = ?, header_image = ?, price_usd = ?, price_inr = ?,
		hltb_main = ?, hltb_extra = ?, hltb_completionist = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`

	_, err := d.db.ExecContext(ctx, query,
		game.GameName, game.Status, game.TimePlayed, game.IsFavorite,
		game.Rating, game.Notes, game.HeaderImage, game.PriceUSD, game.PriceINR,
		game.HLTBMain, game.HLTBExtra, game.HLTBCompletionist, game.UpdatedAt,
		game.ID, game.UserID,
	)
	return err
}

// UpdateUserGameStatus updates just the status of a game
func (d *Database) UpdateUserGameStatus(ctx context.Context, userID, gameID int64, status GameStatus) error {
	now := time.Now().Unix()
	_, err := d.db.ExecContext(ctx,
		"UPDATE user_games SET status = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		status, now, gameID, userID)
	return err
}

// UpdateUserGameTimePlayed updates just the time played
func (d *Database) UpdateUserGameTimePlayed(ctx context.Context, userID, gameID int64, timePlayed float64) error {
	now := time.Now().Unix()
	_, err := d.db.ExecContext(ctx,
		"UPDATE user_games SET time_played = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		timePlayed, now, gameID, userID)
	return err
}

// UpdateUserGameFavorite toggles favorite status
func (d *Database) UpdateUserGameFavorite(ctx context.Context, userID, gameID int64, isFavorite bool) error {
	now := time.Now().Unix()
	fav := 0
	if isFavorite {
		fav = 1
	}
	_, err := d.db.ExecContext(ctx,
		"UPDATE user_games SET is_favorite = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		fav, now, gameID, userID)
	return err
}

// UpdateUserGameRating updates the rating
func (d *Database) UpdateUserGameRating(ctx context.Context, userID, gameID int64, rating *int) error {
	now := time.Now().Unix()
	_, err := d.db.ExecContext(ctx,
		"UPDATE user_games SET rating = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		rating, now, gameID, userID)
	return err
}

// UpdateUserGameNotes updates the notes
func (d *Database) UpdateUserGameNotes(ctx context.Context, userID, gameID int64, notes string) error {
	now := time.Now().Unix()
	var notesVal interface{} = notes
	if notes == "" {
		notesVal = nil
	}
	_, err := d.db.ExecContext(ctx,
		"UPDATE user_games SET notes = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		notesVal, now, gameID, userID)
	return err
}

// RemoveUserGame deletes a game from user's library
func (d *Database) RemoveUserGame(ctx context.Context, userID, gameID int64) error {
	result, err := d.db.ExecContext(ctx,
		"DELETE FROM user_games WHERE id = ? AND user_id = ?", gameID, userID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetUserGameStats calculates user's game statistics
func (d *Database) GetUserGameStats(ctx context.Context, userID int64) (*UserGameStats, error) {
	stats := &UserGameStats{}

	// Get total counts and hours by status
	query := `SELECT
		status,
		COUNT(*) as count,
		COALESCE(SUM(time_played), 0) as hours
		FROM user_games WHERE user_id = ?
		GROUP BY status`

	rows, err := d.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		var hours float64
		if err := rows.Scan(&status, &count, &hours); err != nil {
			return nil, err
		}

		stats.TotalGames += count
		stats.TotalPlaytime += hours

		switch GameStatus(status) {
		case StatusCompleted:
			stats.CompletedCount = count
			stats.CompletedHours = hours
		case StatusPlaying:
			stats.PlayingCount = count
			stats.PlayingHours = hours
		case StatusNotStarted:
			stats.NotStartedCount = count
			stats.BacklogCount += count
		case StatusOnHold:
			stats.OnHoldCount = count
			stats.BacklogCount += count
		case StatusDropped:
			stats.DroppedCount = count
			stats.BacklogCount += count
		case StatusPaused:
			stats.PausedCount = count
			stats.BacklogCount += count
		}
	}

	// Get favorites count
	err = d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_games WHERE user_id = ? AND is_favorite = 1", userID).
		Scan(&stats.TotalFavorites)
	if err != nil {
		return nil, err
	}

	// Get top 3 rated games
	topQuery := `SELECT id, user_id, app_id, game_name, status, time_played, is_favorite,
		rating, notes, header_image, price_usd, price_inr, hltb_main, hltb_extra,
		hltb_completionist, is_steam_game, added_at, updated_at
		FROM user_games WHERE user_id = ? AND rating IS NOT NULL
		ORDER BY rating DESC, updated_at DESC LIMIT 3`

	topRows, err := d.db.QueryContext(ctx, topQuery, userID)
	if err != nil {
		return nil, err
	}
	defer topRows.Close()

	stats.TopRated, err = scanUserGames(topRows)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// GetUserGamesCount returns total count of games for a user
func (d *Database) GetUserGamesCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_games WHERE user_id = ?", userID).Scan(&count)
	return count, err
}

// BulkAddUserGames adds multiple games in a transaction (for Steam import)
func (d *Database) BulkAddUserGames(ctx context.Context, games []UserGame) (int, error) {
	if len(games) == 0 {
		return 0, nil
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO user_games
		(user_id, app_id, game_name, status, time_played, is_favorite, rating, notes,
		 header_image, price_usd, price_inr, hltb_main, hltb_extra, hltb_completionist,
		 is_steam_game, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	inserted := 0

	for _, game := range games {
		result, err := stmt.ExecContext(ctx,
			game.UserID, game.AppID, game.GameName, game.Status, game.TimePlayed,
			game.IsFavorite, game.Rating, game.Notes, game.HeaderImage,
			game.PriceUSD, game.PriceINR, game.HLTBMain, game.HLTBExtra, game.HLTBCompletionist,
			game.IsSteamGame, now, now,
		)
		if err != nil {
			log.Printf("Error inserting game %s: %v", game.GameName, err)
			continue
		}

		rows, _ := result.RowsAffected()
		if rows > 0 {
			inserted++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return inserted, nil
}

// SearchUserGames searches games in user's library by name
func (d *Database) SearchUserGames(ctx context.Context, userID int64, query string, limit int) ([]UserGame, error) {
	sqlQuery := `SELECT id, user_id, app_id, game_name, status, time_played, is_favorite,
		rating, notes, header_image, price_usd, price_inr, hltb_main, hltb_extra,
		hltb_completionist, is_steam_game, added_at, updated_at
		FROM user_games WHERE user_id = ? AND game_name LIKE ?
		ORDER BY game_name ASC LIMIT ?`

	rows, err := d.db.QueryContext(ctx, sqlQuery, userID, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanUserGames(rows)
}
