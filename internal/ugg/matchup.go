package ugg

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// FetchMatchups fetches matchup data for a champion and caches it
func (f *Fetcher) FetchMatchups(championID int, role string) ([]MatchupData, error) {
	f.mu.RLock()
	patch := f.currentPatch
	f.mu.RUnlock()

	if patch == "" {
		if err := f.FetchPatch(); err != nil {
			return nil, err
		}
		f.mu.RLock()
		patch = f.currentPatch
		f.mu.RUnlock()
	}

	// Check cache
	cacheKey := fmt.Sprintf("%d-%s", championID, role)
	f.mu.RLock()
	if cached, ok := f.matchupCache[cacheKey]; ok {
		f.mu.RUnlock()
		return cached, nil
	}
	f.mu.RUnlock()

	// Build URL for matchups
	url := fmt.Sprintf("%s/%s/matchups/%s/ranked_solo_5x5/%d/%s.json",
		statsBaseURL, apiVersion, patch, championID, statsVersion)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch matchup data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("U.GG matchups returned status %d", resp.StatusCode)
	}

	var rawData map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawData); err != nil {
		return nil, fmt.Errorf("failed to parse matchup data: %w", err)
	}

	// Parse and aggregate matchups across all regions
	matchups := f.parseMatchups(rawData, role)

	// Cache it
	f.mu.Lock()
	f.matchupCache[cacheKey] = matchups
	f.mu.Unlock()

	return matchups, nil
}

// parseMatchups aggregates matchup data across all regions
func (f *Fetcher) parseMatchups(rawData map[string]json.RawMessage, role string) []MatchupData {
	roleID := roleToID(role)

	// Aggregate wins/games per enemy champion across all regions
	aggregated := make(map[int]*MatchupData)

	for _, regionData := range rawData {
		var regionMap map[string]json.RawMessage
		if err := json.Unmarshal(regionData, &regionMap); err != nil {
			continue
		}

		roleData, ok := regionMap[roleID]
		if !ok {
			continue
		}

		var tierMap map[string]json.RawMessage
		if err := json.Unmarshal(roleData, &tierMap); err != nil {
			continue
		}

		tierData, ok := tierMap["3"]
		if !ok {
			continue
		}

		var tierContent []json.RawMessage
		if err := json.Unmarshal(tierData, &tierContent); err != nil || len(tierContent) == 0 {
			continue
		}

		var matchups [][]float64
		if err := json.Unmarshal(tierContent[0], &matchups); err != nil {
			continue
		}

		// Aggregate matchups: [enemyChampId, wins, games, ...]
		for _, m := range matchups {
			if len(m) < 3 {
				continue
			}
			enemyID := int(m[0])
			wins := m[1]
			games := m[2]

			if _, exists := aggregated[enemyID]; !exists {
				aggregated[enemyID] = &MatchupData{EnemyChampionID: enemyID}
			}
			aggregated[enemyID].Wins += wins
			aggregated[enemyID].Games += games
		}
	}

	// Convert to slice and calculate win rates
	result := make([]MatchupData, 0, len(aggregated))
	for _, m := range aggregated {
		if m.Games > 0 {
			m.WinRate = (m.Wins / m.Games) * 100
		}
		result = append(result, *m)
	}

	return result
}

// GetMatchupWinRate returns the win rate against a specific enemy champion
func (f *Fetcher) GetMatchupWinRate(championID int, role string, enemyChampionID int) (float64, float64, error) {
	matchups, err := f.FetchMatchups(championID, role)
	if err != nil {
		return 0, 0, err
	}

	for _, m := range matchups {
		if m.EnemyChampionID == enemyChampionID {
			return m.WinRate, m.Games, nil
		}
	}

	return 0, 0, fmt.Errorf("no matchup data for enemy champion %d", enemyChampionID)
}
