package db

import (
	"context"

	json "github.com/goccy/go-json"
)

// Match represents a match record
type Match struct {
	MatchID      string `json:"matchId"`
	GameVersion  string `json:"gameVersion"`
	GameDuration int    `json:"gameDuration"`
	GameCreation int64  `json:"gameCreation"`
}

// Participant represents a player's performance in a match
type Participant struct {
	MatchID      string `json:"matchId"`
	PUUID        string `json:"puuid"`
	GameName     string `json:"gameName"`
	TagLine      string `json:"tagLine"`
	ChampionID   int    `json:"championId"`
	ChampionName string `json:"championName"`
	TeamPosition string `json:"teamPosition"`
	Win          bool   `json:"win"`
	Item0        int    `json:"item0"`
	Item1        int    `json:"item1"`
	Item2        int    `json:"item2"`
	Item3        int    `json:"item3"`
	Item4        int    `json:"item4"`
	Item5        int    `json:"item5"`
	BuildOrder   []int  `json:"buildOrder"`
}

// InsertMatch inserts a match if it doesn't exist
func (db *DB) InsertMatch(ctx context.Context, m *Match) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO matches (match_id, game_version, game_duration, game_creation)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (match_id) DO NOTHING
	`, m.MatchID, m.GameVersion, m.GameDuration, m.GameCreation)
	return err
}

// InsertParticipant inserts a participant record
func (db *DB) InsertParticipant(ctx context.Context, p *Participant) error {
	buildOrderJSON, err := json.Marshal(p.BuildOrder)
	if err != nil {
		return err
	}

	_, err = db.pool.Exec(ctx, `
		INSERT INTO participants (
			match_id, puuid, game_name, tag_line, champion_id, champion_name, team_position, win,
			item0, item1, item2, item3, item4, item5, build_order
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, p.MatchID, p.PUUID, p.GameName, p.TagLine, p.ChampionID, p.ChampionName, p.TeamPosition, p.Win,
		p.Item0, p.Item1, p.Item2, p.Item3, p.Item4, p.Item5, buildOrderJSON)
	return err
}

// MatchExists checks if a match already exists in the database
func (db *DB) MatchExists(ctx context.Context, matchID string) (bool, error) {
	var exists bool
	err := db.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM matches WHERE match_id = $1)
	`, matchID).Scan(&exists)
	return exists, err
}

// GetMatchCount returns the total number of matches
func (db *DB) GetMatchCount(ctx context.Context) (int, error) {
	var count int
	err := db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM matches`).Scan(&count)
	return count, err
}

// GetParticipantCount returns the total number of participants
func (db *DB) GetParticipantCount(ctx context.Context) (int, error) {
	var count int
	err := db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM participants`).Scan(&count)
	return count, err
}

// ChampionBuildStats represents aggregated build stats for a champion
type ChampionBuildStats struct {
	ChampionID   int     `json:"championId"`
	ChampionName string  `json:"championName"`
	TotalGames   int     `json:"totalGames"`
	Wins         int     `json:"wins"`
	WinRate      float64 `json:"winRate"`
}

// GetChampionStats returns aggregate stats for all champions
func (db *DB) GetChampionStats(ctx context.Context) ([]ChampionBuildStats, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT
			champion_id,
			champion_name,
			COUNT(*) as total_games,
			SUM(CASE WHEN win THEN 1 ELSE 0 END) as wins
		FROM participants
		GROUP BY champion_id, champion_name
		ORDER BY total_games DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []ChampionBuildStats
	for rows.Next() {
		var s ChampionBuildStats
		if err := rows.Scan(&s.ChampionID, &s.ChampionName, &s.TotalGames, &s.Wins); err != nil {
			return nil, err
		}
		if s.TotalGames > 0 {
			s.WinRate = float64(s.Wins) / float64(s.TotalGames) * 100
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// GetRecentMatches returns the most recent matches
func (db *DB) GetRecentMatches(ctx context.Context, limit int) ([]Match, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT match_id, game_version, game_duration, game_creation
		FROM matches
		ORDER BY game_creation DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.MatchID, &m.GameVersion, &m.GameDuration, &m.GameCreation); err != nil {
			return nil, err
		}
		matches = append(matches, m)
	}
	return matches, nil
}

// MatchDetail contains match info with all participants
type MatchDetail struct {
	Match
	Participants []ParticipantDetail `json:"participants"`
}

// ParticipantDetail contains participant info for display
type ParticipantDetail struct {
	GameName     string `json:"gameName"`
	TagLine      string `json:"tagLine"`
	ChampionID   int    `json:"championId"`
	ChampionName string `json:"championName"`
	TeamPosition string `json:"teamPosition"`
	Win          bool   `json:"win"`
	Items        []int  `json:"items"`
	BuildOrder   []int  `json:"buildOrder"`
}

// GetMatchDetail returns a match with all participants
func (db *DB) GetMatchDetail(ctx context.Context, matchID string) (*MatchDetail, error) {
	// Get match info
	var m MatchDetail
	err := db.pool.QueryRow(ctx, `
		SELECT match_id, game_version, game_duration, game_creation
		FROM matches WHERE match_id = $1
	`, matchID).Scan(&m.MatchID, &m.GameVersion, &m.GameDuration, &m.GameCreation)
	if err != nil {
		return nil, err
	}

	// Get participants
	rows, err := db.pool.Query(ctx, `
		SELECT COALESCE(game_name, ''), COALESCE(tag_line, ''),
		       champion_id, champion_name, team_position, win,
		       item0, item1, item2, item3, item4, item5, build_order
		FROM participants
		WHERE match_id = $1
		ORDER BY
			CASE team_position
				WHEN 'TOP' THEN 1
				WHEN 'JUNGLE' THEN 2
				WHEN 'MIDDLE' THEN 3
				WHEN 'BOTTOM' THEN 4
				WHEN 'UTILITY' THEN 5
				ELSE 6
			END,
			win DESC
	`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p ParticipantDetail
		var item0, item1, item2, item3, item4, item5 int
		var buildOrderJSON []byte

		err := rows.Scan(&p.GameName, &p.TagLine, &p.ChampionID, &p.ChampionName, &p.TeamPosition, &p.Win,
			&item0, &item1, &item2, &item3, &item4, &item5, &buildOrderJSON)
		if err != nil {
			return nil, err
		}

		p.Items = []int{item0, item1, item2, item3, item4, item5}

		// Parse build order JSON
		if len(buildOrderJSON) > 0 {
			json.Unmarshal(buildOrderJSON, &p.BuildOrder)
		}

		m.Participants = append(m.Participants, p)
	}

	return &m, nil
}
