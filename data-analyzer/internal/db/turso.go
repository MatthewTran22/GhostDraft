package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// TursoClient wraps a connection to Turso
type TursoClient struct {
	db *sql.DB
}

// NewTursoClient creates a new Turso client
func NewTursoClient(url, authToken string) (*TursoClient, error) {
	connStr := url
	if authToken != "" {
		connStr = fmt.Sprintf("%s?authToken=%s", url, authToken)
	}

	db, err := sql.Open("libsql", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Turso: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping Turso: %w", err)
	}

	return &TursoClient{db: db}, nil
}

// Close closes the Turso connection
func (c *TursoClient) Close() error {
	return c.db.Close()
}

// CreateTables creates the required tables if they don't exist (without indexes for bulk loading)
func (c *TursoClient) CreateTables(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS data_version (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			patch TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS champion_stats (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position)
		)`,
		`CREATE TABLE IF NOT EXISTS champion_items (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			item_id INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS champion_item_slots (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			item_id INTEGER NOT NULL,
			build_slot INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, item_id, build_slot)
		)`,
		`CREATE TABLE IF NOT EXISTS champion_matchups (
			patch TEXT NOT NULL,
			champion_id INTEGER NOT NULL,
			team_position TEXT NOT NULL,
			enemy_champion_id INTEGER NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			matches INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (patch, champion_id, team_position, enemy_champion_id)
		)`,
		// Note: Indexes are created separately via CreateIndexes() for bulk loading optimization
	}

	for _, query := range queries {
		if _, err := c.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
	}

	return nil
}

// CreateTablesWithIndexes creates tables and indexes (for normal operation, not bulk loading)
func (c *TursoClient) CreateTablesWithIndexes(ctx context.Context) error {
	if err := c.CreateTables(ctx); err != nil {
		return err
	}
	return c.CreateIndexes(ctx)
}

// ClearData deletes all existing data using a single batched transaction
func (c *TursoClient) ClearData(ctx context.Context) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables := []string{"data_version", "champion_stats", "champion_items", "champion_item_slots", "champion_matchups"}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("failed to clear %s: %w", table, err)
		}
	}

	return tx.Commit()
}

// SetDataVersion sets the current patch version
func (c *TursoClient) SetDataVersion(ctx context.Context, patch string) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO data_version (id, patch, updated_at) VALUES (1, ?, ?)`,
		patch, time.Now().UTC().Format(time.RFC3339))
	return err
}

// ChampionStat represents a champion stat row
type ChampionStat struct {
	Patch        string
	ChampionID   int
	TeamPosition string
	Wins         int
	Matches      int
}

// ChampionItem represents a champion item row
type ChampionItem struct {
	Patch        string
	ChampionID   int
	TeamPosition string
	ItemID       int
	Wins         int
	Matches      int
}

// ChampionItemSlot represents a champion item slot row
type ChampionItemSlot struct {
	Patch        string
	ChampionID   int
	TeamPosition string
	ItemID       int
	BuildSlot    int
	Wins         int
	Matches      int
}

// ChampionMatchup represents a champion matchup row
type ChampionMatchup struct {
	Patch           string
	ChampionID      int
	TeamPosition    string
	EnemyChampionID int
	Wins            int
	Matches         int
}

const batchSize = 500 // Increased for bulk loading performance

// InsertChampionStats inserts champion stats using multi-value INSERT
func (c *TursoClient) InsertChampionStats(ctx context.Context, stats []ChampionStat) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := 0; i < len(stats); i += batchSize {
		end := i + batchSize
		if end > len(stats) {
			end = len(stats)
		}
		batch := stats[i:end]

		// Build multi-value INSERT: INSERT INTO table VALUES (?,?,?), (?,?,?), ...
		placeholders := make([]string, len(batch))
		args := make([]interface{}, 0, len(batch)*5)

		for j, s := range batch {
			placeholders[j] = "(?, ?, ?, ?, ?)"
			args = append(args, s.Patch, s.ChampionID, s.TeamPosition, s.Wins, s.Matches)
		}

		query := fmt.Sprintf(
			`INSERT INTO champion_stats (patch, champion_id, team_position, wins, matches) VALUES %s
			ON CONFLICT(patch, champion_id, team_position) DO UPDATE SET
				wins = wins + excluded.wins,
				matches = matches + excluded.matches`,
			strings.Join(placeholders, ", "))

		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// InsertChampionItems inserts champion items using upsert
func (c *TursoClient) InsertChampionItems(ctx context.Context, items []ChampionItem) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, 0, len(batch)*6)

		for j, item := range batch {
			placeholders[j] = "(?, ?, ?, ?, ?, ?)"
			args = append(args, item.Patch, item.ChampionID, item.TeamPosition, item.ItemID, item.Wins, item.Matches)
		}

		query := fmt.Sprintf(
			`INSERT INTO champion_items (patch, champion_id, team_position, item_id, wins, matches) VALUES %s
			ON CONFLICT(patch, champion_id, team_position, item_id) DO UPDATE SET
				wins = wins + excluded.wins,
				matches = matches + excluded.matches`,
			strings.Join(placeholders, ", "))

		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// InsertChampionItemSlots inserts champion item slots using upsert
func (c *TursoClient) InsertChampionItemSlots(ctx context.Context, slots []ChampionItemSlot) error {
	if len(slots) == 0 {
		return nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := 0; i < len(slots); i += batchSize {
		end := i + batchSize
		if end > len(slots) {
			end = len(slots)
		}
		batch := slots[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, 0, len(batch)*7)

		for j, slot := range batch {
			placeholders[j] = "(?, ?, ?, ?, ?, ?, ?)"
			args = append(args, slot.Patch, slot.ChampionID, slot.TeamPosition, slot.ItemID, slot.BuildSlot, slot.Wins, slot.Matches)
		}

		query := fmt.Sprintf(
			`INSERT INTO champion_item_slots (patch, champion_id, team_position, item_id, build_slot, wins, matches) VALUES %s
			ON CONFLICT(patch, champion_id, team_position, item_id, build_slot) DO UPDATE SET
				wins = wins + excluded.wins,
				matches = matches + excluded.matches`,
			strings.Join(placeholders, ", "))

		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// InsertChampionMatchups inserts champion matchups using upsert
func (c *TursoClient) InsertChampionMatchups(ctx context.Context, matchups []ChampionMatchup) error {
	if len(matchups) == 0 {
		return nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := 0; i < len(matchups); i += batchSize {
		end := i + batchSize
		if end > len(matchups) {
			end = len(matchups)
		}
		batch := matchups[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, 0, len(batch)*6)

		for j, m := range batch {
			placeholders[j] = "(?, ?, ?, ?, ?, ?)"
			args = append(args, m.Patch, m.ChampionID, m.TeamPosition, m.EnemyChampionID, m.Wins, m.Matches)
		}

		query := fmt.Sprintf(
			`INSERT INTO champion_matchups (patch, champion_id, team_position, enemy_champion_id, wins, matches) VALUES %s
			ON CONFLICT(patch, champion_id, team_position, enemy_champion_id) DO UPDATE SET
				wins = wins + excluded.wins,
				matches = matches + excluded.matches`,
			strings.Join(placeholders, ", "))

		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetDataVersion returns the current data version from the database
func (c *TursoClient) GetDataVersion(ctx context.Context) (string, error) {
	var version string
	err := c.db.QueryRowContext(ctx, "SELECT patch FROM data_version WHERE id = 1").Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // No version set yet
		}
		return "", err
	}
	return version, nil
}

// Index definitions for bulk loading optimization
var indexDefinitions = []string{
	`CREATE INDEX IF NOT EXISTS idx_champion_stats_position ON champion_stats(team_position)`,
	`CREATE INDEX IF NOT EXISTS idx_champion_stats_champ_pos ON champion_stats(champion_id, team_position)`,
	`CREATE INDEX IF NOT EXISTS idx_champion_items_champ_pos ON champion_items(champion_id, team_position)`,
	`CREATE INDEX IF NOT EXISTS idx_champion_item_slots_champ_pos ON champion_item_slots(champion_id, team_position)`,
	`CREATE INDEX IF NOT EXISTS idx_champion_item_slots_champ_pos_slot ON champion_item_slots(champion_id, team_position, build_slot)`,
	`CREATE INDEX IF NOT EXISTS idx_champion_matchups_champ_pos ON champion_matchups(champion_id, team_position)`,
	`CREATE INDEX IF NOT EXISTS idx_champion_matchups_enemy ON champion_matchups(champion_id, team_position, enemy_champion_id)`,
}

var indexNames = []string{
	"idx_champion_stats_position",
	"idx_champion_stats_champ_pos",
	"idx_champion_items_champ_pos",
	"idx_champion_item_slots_champ_pos",
	"idx_champion_item_slots_champ_pos_slot",
	"idx_champion_matchups_champ_pos",
	"idx_champion_matchups_enemy",
}

// DropIndexes drops all indexes for faster bulk inserts
func (c *TursoClient) DropIndexes(ctx context.Context) error {
	fmt.Println("Dropping indexes for bulk loading...")
	for _, name := range indexNames {
		query := fmt.Sprintf("DROP INDEX IF EXISTS %s", name)
		if _, err := c.db.ExecContext(ctx, query); err != nil {
			// Log but don't fail - index might not exist
			fmt.Printf("  Warning: failed to drop index %s: %v\n", name, err)
		}
	}
	fmt.Printf("  Dropped %d indexes\n", len(indexNames))
	return nil
}

// CreateIndexes creates all indexes after bulk insert
func (c *TursoClient) CreateIndexes(ctx context.Context) error {
	fmt.Println("Creating indexes after bulk load...")
	for _, query := range indexDefinitions {
		if _, err := c.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}
	fmt.Printf("  Created %d indexes\n", len(indexDefinitions))
	return nil
}

// DeleteOldPatches removes data from patches older than minPatch using a single transaction
func (c *TursoClient) DeleteOldPatches(ctx context.Context, minPatch string) (int64, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	tables := []string{"champion_stats", "champion_items", "champion_item_slots", "champion_matchups"}
	var totalDeleted int64

	for _, table := range tables {
		result, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE patch < ?", table), minPatch)
		if err != nil {
			return 0, fmt.Errorf("failed to delete from %s: %w", table, err)
		}
		rows, _ := result.RowsAffected()
		totalDeleted += rows
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return totalDeleted, nil
}
