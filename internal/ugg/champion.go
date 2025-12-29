package ugg

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// FetchChampionData fetches build data for a champion
func (f *Fetcher) FetchChampionData(championID int, championName string, role string) (*BuildData, error) {
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
	if cached, ok := f.cache[cacheKey]; ok {
		f.mu.RUnlock()
		return cached, nil
	}
	f.mu.RUnlock()

	// Build URL: https://stats2.u.gg/lol/1.5/overview/15_24/ranked_solo_5x5/233/1.5.0.json
	url := fmt.Sprintf("%s/%s/overview/%s/ranked_solo_5x5/%d/%s.json",
		statsBaseURL, apiVersion, patch, championID, statsVersion)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch champion data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("U.GG returned status %d", resp.StatusCode)
	}

	// Parse the response - U.GG returns nested data by region/role
	var rawData map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawData); err != nil {
		return nil, fmt.Errorf("failed to parse champion data: %w", err)
	}

	// Parse the build data
	buildData, err := f.parseChampionData(rawData, championID, championName, role)
	if err != nil {
		return nil, err
	}

	// Cache it
	f.mu.Lock()
	f.cache[cacheKey] = buildData
	f.mu.Unlock()

	return buildData, nil
}

// parseChampionData extracts build data from U.GG response
func (f *Fetcher) parseChampionData(rawData map[string]json.RawMessage, championID int, championName string, role string) (*BuildData, error) {
	roleID := roleToID(role)

	// Aggregate wins/games across all regions for the target role
	var totalWins, totalGames float64
	var bestStatsData []json.RawMessage
	var bestGames float64

	for regionID, regionData := range rawData {
		var regionMap map[string]json.RawMessage
		if err := json.Unmarshal(regionData, &regionMap); err != nil {
			continue
		}

		rolesToTry := []string{roleID}
		if roleID == "" {
			rolesToTry = []string{"5", "4", "3", "1", "2"}
		}

		for _, tryRole := range rolesToTry {
			if tryRole == "" {
				continue
			}
			roleData, ok := regionMap[tryRole]
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
			if err := json.Unmarshal(tierData, &tierContent); err != nil {
				continue
			}

			if len(tierContent) > 0 {
				var statsData []json.RawMessage
				if err := json.Unmarshal(tierContent[0], &statsData); err != nil {
					continue
				}
				if len(statsData) > 6 {
					wins, games := f.getWinsAndGames(statsData[6])
					if games > 0 && tryRole == roleID {
						totalWins += wins
						totalGames += games
						fmt.Printf("Region %s, Role %s: %.0f wins / %.0f games\n", regionID, tryRole, wins, games)

						if games > bestGames {
							bestGames = games
							bestStatsData = statsData
						}
					}
				}
			}
			if tryRole == roleID {
				break
			}
		}
	}

	// If no data for target role, try to find any role
	if len(bestStatsData) == 0 {
		for _, regionData := range rawData {
			var regionMap map[string]json.RawMessage
			if err := json.Unmarshal(regionData, &regionMap); err != nil {
				continue
			}
			for _, tryRole := range []string{"5", "4", "3", "1", "2"} {
				roleData, ok := regionMap[tryRole]
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
				var statsData []json.RawMessage
				if err := json.Unmarshal(tierContent[0], &statsData); err != nil {
					continue
				}
				if len(statsData) > 6 {
					wins, games := f.getWinsAndGames(statsData[6])
					if games > bestGames {
						bestGames = games
						bestStatsData = statsData
						totalWins = wins
						totalGames = games
					}
				}
			}
			if len(bestStatsData) > 0 {
				break
			}
		}
	}

	if len(bestStatsData) == 0 {
		return nil, fmt.Errorf("no data found for champion %d", championID)
	}

	statsData := bestStatsData
	fmt.Printf("Aggregated: %.0f wins / %.0f games = %.1f%% WR\n", totalWins, totalGames, (totalWins/totalGames)*100)

	build := &BuildData{
		ChampionID:   championID,
		ChampionName: championName,
		Role:         role,
	}

	if totalGames > 0 {
		build.WinRate = (totalWins / totalGames) * 100
		build.PickRate = totalGames
	}

	// Parse starting items (index 2)
	if len(statsData) > 2 {
		f.parseItems(statsData[2], &build.StartingItems)
	}

	// Parse core items (index 3)
	if len(statsData) > 3 {
		f.parseItems(statsData[3], &build.CoreItems)
	}

	return build, nil
}

// parseItems extracts item IDs
func (f *Fetcher) parseItems(data json.RawMessage, items *[]int) {
	var itemData []json.RawMessage
	if err := json.Unmarshal(data, &itemData); err != nil || len(itemData) == 0 {
		return
	}

	var itemArray []json.RawMessage
	if err := json.Unmarshal(itemData[0], &itemArray); err != nil || len(itemArray) < 3 {
		return
	}

	var itemIDs []int
	if err := json.Unmarshal(itemArray[2], &itemIDs); err != nil {
		return
	}

	*items = itemIDs
}
