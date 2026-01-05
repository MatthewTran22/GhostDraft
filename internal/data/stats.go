package data

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultManifestURL is the default URL for fetching stats data
const DefaultManifestURL = "https://raw.githubusercontent.com/MatthewTran22/LoLOverlay-Data/main/manifest.json"

// StatsDB manages the stats database with remote update capability
type StatsDB struct {
	db           *sql.DB
	currentPatch string
}

// Manifest represents the remote manifest.json structure
type Manifest struct {
	Version     string `json:"version"`
	DataURL     string `json:"data_url"`
	DataSha256  string `json:"data_sha256"`
	UpdatedAt   string `json:"updated_at"`
	ForceReset  bool   `json:"force_reset"`
}

// DataExport represents the data.json structure from the reducer
type DataExport struct {
	Patch             string                 `json:"patch"`
	GeneratedAt       string                 `json:"generatedAt"`
	ChampionStats     []ChampionStatJSON     `json:"championStats"`
	ChampionItems     []ChampionItemJSON     `json:"championItems"`
	ChampionItemSlots []ChampionItemSlotJSON `json:"championItemSlots"`
	ChampionMatchups  []ChampionMatchupJSON  `json:"championMatchups"`
}

type ChampionStatJSON struct {
	Patch        string `json:"patch"`
	ChampionID   int    `json:"championId"`
	TeamPosition string `json:"teamPosition"`
	Wins         int    `json:"wins"`
	Matches      int    `json:"matches"`
}

type ChampionItemJSON struct {
	Patch        string `json:"patch"`
	ChampionID   int    `json:"championId"`
	TeamPosition string `json:"teamPosition"`
	ItemID       int    `json:"itemId"`
	Wins         int    `json:"wins"`
	Matches      int    `json:"matches"`
}

type ChampionItemSlotJSON struct {
	Patch        string `json:"patch"`
	ChampionID   int    `json:"championId"`
	TeamPosition string `json:"teamPosition"`
	ItemID       int    `json:"itemId"`
	BuildSlot    int    `json:"buildSlot"`
	Wins         int    `json:"wins"`
	Matches      int    `json:"matches"`
}

type ChampionMatchupJSON struct {
	Patch           string `json:"patch"`
	ChampionID      int    `json:"championId"`
	TeamPosition    string `json:"teamPosition"`
	EnemyChampionID int    `json:"enemyChampionId"`
	Wins            int    `json:"wins"`
	Matches         int    `json:"matches"`
}

// NewStatsDB creates and initializes the stats database
func NewStatsDB() (*StatsDB, error) {
	// Get user's app data directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}

	dbDir := filepath.Join(configDir, "GhostDraft")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	dbPath := filepath.Join(dbDir, "stats.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sdb := &StatsDB{db: db}
	if err := sdb.init(); err != nil {
		db.Close()
		return nil, err
	}

	// Load current patch from database
	sdb.loadCurrentPatch()

	return sdb, nil
}

// init creates the schema
func (s *StatsDB) init() error {
	schema := `
		CREATE TABLE IF NOT EXISTS data_version (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			patch TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS champion_stats (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position)
		);

		CREATE TABLE IF NOT EXISTS champion_items (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			item_id INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, item_id)
		);

		CREATE TABLE IF NOT EXISTS champion_item_slots (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			item_id INTEGER NOT NULL,
			build_slot INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, item_id, build_slot)
		);

		CREATE TABLE IF NOT EXISTS champion_matchups (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			enemy_champion_id INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, enemy_champion_id)
		);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// loadCurrentPatch loads the current patch from the database
func (s *StatsDB) loadCurrentPatch() {
	var patch string
	err := s.db.QueryRow("SELECT patch FROM data_version WHERE id = 1").Scan(&patch)
	if err == nil {
		s.currentPatch = patch
	}
}

// GetCurrentPatch returns the locally stored patch version
func (s *StatsDB) GetCurrentPatch() string {
	return s.currentPatch
}

// GetDB returns the underlying database connection
func (s *StatsDB) GetDB() *sql.DB {
	return s.db
}

// CheckForUpdates fetches the remote manifest and updates if newer version available
func (s *StatsDB) CheckForUpdates(manifestURL string) error {
	if manifestURL == "" {
		return fmt.Errorf("manifest URL not configured")
	}

	fmt.Printf("[Stats] Checking for updates from: %s\n", manifestURL)

	// Fetch manifest with timeout
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(manifestURL)
	if err != nil {
		return fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("manifest fetch returned status %d", resp.StatusCode)
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	fmt.Printf("[Stats] Remote version: %s, Local patch: %s, ForceReset: %v\n", manifest.Version, s.currentPatch, manifest.ForceReset)

	// Force reset clears local data before comparing versions
	if manifest.ForceReset {
		fmt.Println("[Stats] Force reset requested - clearing local data")
		s.clearLocalData()
	}

	// Compare versions (simple string comparison works for semantic versions like "14.24.1")
	if manifest.Version != "" && manifest.Version <= s.currentPatch {
		fmt.Println("[Stats] Local data is up to date")
		return nil
	}

	// Download and import new data
	fmt.Printf("[Stats] Downloading new data from: %s\n", manifest.DataURL)
	if err := s.downloadAndImport(manifest.DataURL, manifest.DataSha256, manifest.Version); err != nil {
		return fmt.Errorf("failed to download and import data: %w", err)
	}

	// Reload current patch from database after import
	s.loadCurrentPatch()
	fmt.Printf("[Stats] Updated to version: %s\n", s.currentPatch)
	return nil
}

// downloadAndImport downloads data.json and imports it into SQLite
func (s *StatsDB) downloadAndImport(dataURL, expectedSha256, manifestVersion string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(dataURL)
	if err != nil {
		return fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("data fetch returned status %d", resp.StatusCode)
	}

	// Read and parse JSON
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read data: %w", err)
	}

	// Verify SHA256 hash if provided
	if expectedSha256 != "" {
		hasher := sha256.New()
		hasher.Write(body)
		actualSha256 := hex.EncodeToString(hasher.Sum(nil))

		if actualSha256 != expectedSha256 {
			return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSha256, actualSha256)
		}
		fmt.Println("[Stats] SHA256 verified successfully")
	}

	var data DataExport
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("failed to parse data: %w", err)
	}

	// Use manifest version for tracking updates
	return s.ImportData(&data, manifestVersion)
}

// ImportData bulk inserts data into SQLite using a single transaction
// Uses upsert to accumulate data instead of replacing
func (s *StatsDB) ImportData(data *DataExport, version string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after Commit()

	// Upsert champion_stats - add to existing values on conflict
	stmtStats, err := tx.Prepare(`
		INSERT INTO champion_stats (patch, champion_id, team_position, wins, matches)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(patch, champion_id, team_position) DO UPDATE SET
			wins = wins + excluded.wins,
			matches = matches + excluded.matches
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare champion_stats statement: %w", err)
	}
	defer stmtStats.Close()

	for _, stat := range data.ChampionStats {
		if _, err := stmtStats.Exec(stat.Patch, stat.ChampionID, stat.TeamPosition, stat.Wins, stat.Matches); err != nil {
			return fmt.Errorf("failed to insert champion_stats: %w", err)
		}
	}

	// Upsert champion_items - add to existing values on conflict
	stmtItems, err := tx.Prepare(`
		INSERT INTO champion_items (patch, champion_id, team_position, item_id, wins, matches)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(patch, champion_id, team_position, item_id) DO UPDATE SET
			wins = wins + excluded.wins,
			matches = matches + excluded.matches
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare champion_items statement: %w", err)
	}
	defer stmtItems.Close()

	for _, item := range data.ChampionItems {
		if _, err := stmtItems.Exec(item.Patch, item.ChampionID, item.TeamPosition, item.ItemID, item.Wins, item.Matches); err != nil {
			return fmt.Errorf("failed to insert champion_items: %w", err)
		}
	}

	// Upsert champion_item_slots - add to existing values on conflict
	stmtItemSlots, err := tx.Prepare(`
		INSERT INTO champion_item_slots (patch, champion_id, team_position, item_id, build_slot, wins, matches)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(patch, champion_id, team_position, item_id, build_slot) DO UPDATE SET
			wins = wins + excluded.wins,
			matches = matches + excluded.matches
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare champion_item_slots statement: %w", err)
	}
	defer stmtItemSlots.Close()

	for _, slot := range data.ChampionItemSlots {
		if _, err := stmtItemSlots.Exec(slot.Patch, slot.ChampionID, slot.TeamPosition, slot.ItemID, slot.BuildSlot, slot.Wins, slot.Matches); err != nil {
			return fmt.Errorf("failed to insert champion_item_slots: %w", err)
		}
	}

	// Upsert champion_matchups - add to existing values on conflict
	stmtMatchups, err := tx.Prepare(`
		INSERT INTO champion_matchups (patch, champion_id, team_position, enemy_champion_id, wins, matches)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(patch, champion_id, team_position, enemy_champion_id) DO UPDATE SET
			wins = wins + excluded.wins,
			matches = matches + excluded.matches
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare champion_matchups statement: %w", err)
	}
	defer stmtMatchups.Close()

	for _, matchup := range data.ChampionMatchups {
		if _, err := stmtMatchups.Exec(matchup.Patch, matchup.ChampionID, matchup.TeamPosition, matchup.EnemyChampionID, matchup.Wins, matchup.Matches); err != nil {
			return fmt.Errorf("failed to insert champion_matchups: %w", err)
		}
	}

	// Update version - use manifest version for tracking updates
	if _, err := tx.Exec(`
		INSERT OR REPLACE INTO data_version (id, patch, updated_at)
		VALUES (1, ?, datetime('now'))
	`, version); err != nil {
		return fmt.Errorf("failed to update version: %w", err)
	}

	// Commit transaction - single disk sync
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("[Stats] Accumulated: %d champion stats, %d item stats, %d item slot stats, %d matchup stats\n",
		len(data.ChampionStats), len(data.ChampionItems), len(data.ChampionItemSlots), len(data.ChampionMatchups))

	return nil
}

// HasData checks if the database has any stats data
func (s *StatsDB) HasData() bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM champion_stats").Scan(&count)
	return err == nil && count > 0
}

// clearLocalData clears the version tracking to force a redownload
func (s *StatsDB) clearLocalData() {
	s.db.Exec("DELETE FROM data_version")
	s.currentPatch = ""
}

// ForceUpdate clears local version and triggers a fresh download
func (s *StatsDB) ForceUpdate(manifestURL string) error {
	fmt.Println("[Stats] Force update requested - clearing local version")
	s.clearLocalData()
	return s.CheckForUpdates(manifestURL)
}

// Close closes the database connection
func (s *StatsDB) Close() error {
	return s.db.Close()
}
