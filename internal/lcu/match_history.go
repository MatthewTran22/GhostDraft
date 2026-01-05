package lcu

import (
	"encoding/json"
	"fmt"
	"io"
)

// MatchHistoryResponse represents the LCU match history response
type MatchHistoryResponse struct {
	Games struct {
		Games []MatchGame `json:"games"`
	} `json:"games"`
}

// MatchGame represents a single game in match history
type MatchGame struct {
	GameId       int64             `json:"gameId"`
	GameCreation int64             `json:"gameCreation"`
	GameDuration int               `json:"gameDuration"`
	QueueId      int               `json:"queueId"`
	GameMode     string            `json:"gameMode"`
	GameType     string            `json:"gameType"`
	Participants []MatchParticipant `json:"participants"`
}

// MatchParticipant represents a participant in a match
type MatchParticipant struct {
	ChampionId int                `json:"championId"`
	Stats      ParticipantStats   `json:"stats"`
	Timeline   ParticipantTimeline `json:"timeline"`
}

// ParticipantTimeline contains role/lane info
type ParticipantTimeline struct {
	Lane string `json:"lane"`
	Role string `json:"role"`
}

// ParticipantStats represents the stats for a participant
type ParticipantStats struct {
	Win                    bool `json:"win"`
	Kills                  int  `json:"kills"`
	Deaths                 int  `json:"deaths"`
	Assists                int  `json:"assists"`
	TotalMinionsKilled     int  `json:"totalMinionsKilled"`
	NeutralMinionsKilled   int  `json:"neutralMinionsKilled"`
}

// PersonalStats represents aggregated personal statistics
type PersonalStats struct {
	HasData          bool                    `json:"hasData"`
	TotalGames       int                     `json:"totalGames"`
	Wins             int                     `json:"wins"`
	Losses           int                     `json:"losses"`
	WinRate          float64                 `json:"winRate"`
	AvgKills         float64                 `json:"avgKills"`
	AvgDeaths        float64                 `json:"avgDeaths"`
	AvgAssists       float64                 `json:"avgAssists"`
	AvgKDA           float64                 `json:"avgKDA"`
	AvgCS            float64                 `json:"avgCS"`
	AvgCSPerMin      float64                 `json:"avgCSPerMin"`
	ChampionStats    []ChampionPersonalStats `json:"championStats"`
}

// ChampionPersonalStats represents stats for a specific champion
type ChampionPersonalStats struct {
	ChampionId    int            `json:"championId"`
	ChampionName  string         `json:"championName"`
	IconURL       string         `json:"iconURL"`
	SplashURL     string         `json:"splashURL"`
	Role          string         `json:"role"`
	RoleIconURL   string         `json:"roleIconURL"`
	Games         int            `json:"games"`
	Wins          int            `json:"wins"`
	WinRate       float64        `json:"winRate"`
	AvgKills      float64        `json:"avgKills"`
	AvgDeaths     float64        `json:"avgDeaths"`
	AvgAssists    float64        `json:"avgAssists"`
	AvgKDA        float64        `json:"avgKDA"`
	AvgCS         float64        `json:"avgCS"`
	AvgCSPerMin   float64        `json:"avgCSPerMin"`
	TotalDuration int            `json:"-"`          // internal tracking
	RoleCounts    map[string]int `json:"-"`          // internal tracking
}

// Role icon URLs from Community Dragon
var roleIconURLs = map[string]string{
	"TOP":     "https://raw.communitydragon.org/latest/plugins/rcp-fe-lol-clash/global/default/assets/images/position-selector/positions/icon-position-top.png",
	"JUNGLE":  "https://raw.communitydragon.org/latest/plugins/rcp-fe-lol-clash/global/default/assets/images/position-selector/positions/icon-position-jungle.png",
	"MID":     "https://raw.communitydragon.org/latest/plugins/rcp-fe-lol-clash/global/default/assets/images/position-selector/positions/icon-position-middle.png",
	"ADC":     "https://raw.communitydragon.org/latest/plugins/rcp-fe-lol-clash/global/default/assets/images/position-selector/positions/icon-position-bottom.png",
	"SUPPORT": "https://raw.communitydragon.org/latest/plugins/rcp-fe-lol-clash/global/default/assets/images/position-selector/positions/icon-position-utility.png",
}

// normalizeRole converts LCU lane/role to a standard role name
func normalizeRole(lane, role string) string {
	switch lane {
	case "TOP":
		return "TOP"
	case "JUNGLE":
		return "JUNGLE"
	case "MIDDLE", "MID":
		return "MID"
	case "BOTTOM":
		if role == "DUO_CARRY" || role == "CARRY" {
			return "ADC"
		}
		return "SUPPORT"
	default:
		// Fallback based on role
		if role == "DUO_SUPPORT" || role == "SUPPORT" {
			return "SUPPORT"
		}
		return "MID" // Default fallback
	}
}

// FetchMatchHistory fetches match history from the LCU
func (c *Client) FetchMatchHistory(count int) (*MatchHistoryResponse, error) {
	endpoint := fmt.Sprintf("/lol-match-history/v1/products/lol/current-summoner/matches?begIndex=0&endIndex=%d", count)

	resp, err := c.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch match history: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("match history request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var history MatchHistoryResponse
	if err := json.Unmarshal(body, &history); err != nil {
		return nil, fmt.Errorf("failed to parse match history: %w", err)
	}

	return &history, nil
}

// CalculatePersonalStats calculates aggregated stats from match history
func CalculatePersonalStats(history *MatchHistoryResponse, champRegistry *ChampionRegistry) *PersonalStats {
	stats := &PersonalStats{
		HasData:       false,
		ChampionStats: []ChampionPersonalStats{},
	}

	if history == nil || len(history.Games.Games) == 0 {
		return stats
	}

	// Filter to only ranked games (queue IDs: 420=ranked solo, 440=ranked flex)
	validQueues := map[int]bool{420: true, 440: true}

	var totalKills, totalDeaths, totalAssists, totalCS int
	var totalGameDuration int
	champData := make(map[int]*ChampionPersonalStats)

	for _, game := range history.Games.Games {
		if !validQueues[game.QueueId] {
			continue
		}

		// The first participant is always the current player in LCU match history
		if len(game.Participants) == 0 {
			continue
		}

		p := game.Participants[0]
		s := p.Stats

		stats.TotalGames++
		if s.Win {
			stats.Wins++
		} else {
			stats.Losses++
		}

		totalKills += s.Kills
		totalDeaths += s.Deaths
		totalAssists += s.Assists
		totalCS += s.TotalMinionsKilled + s.NeutralMinionsKilled
		totalGameDuration += game.GameDuration

		// Track per-champion stats
		champId := p.ChampionId
		if _, exists := champData[champId]; !exists {
			champName := ""
			iconURL := ""
			splashURL := ""
			if champRegistry != nil {
				champName = champRegistry.GetName(champId)
				iconURL = champRegistry.GetIconURL(champId)
				splashURL = champRegistry.GetSplashURL(champId)
			}
			champData[champId] = &ChampionPersonalStats{
				ChampionId:   champId,
				ChampionName: champName,
				IconURL:      iconURL,
				SplashURL:    splashURL,
				RoleCounts:   make(map[string]int),
			}
		}
		cd := champData[champId]
		cd.Games++
		if s.Win {
			cd.Wins++
		}
		cd.AvgKills += float64(s.Kills)
		cd.AvgDeaths += float64(s.Deaths)
		cd.AvgAssists += float64(s.Assists)
		cd.AvgCS += float64(s.TotalMinionsKilled + s.NeutralMinionsKilled)
		cd.TotalDuration += game.GameDuration

		// Track role
		role := normalizeRole(p.Timeline.Lane, p.Timeline.Role)
		cd.RoleCounts[role]++
	}

	if stats.TotalGames == 0 {
		return stats
	}

	stats.HasData = true
	stats.WinRate = float64(stats.Wins) / float64(stats.TotalGames) * 100
	stats.AvgKills = float64(totalKills) / float64(stats.TotalGames)
	stats.AvgDeaths = float64(totalDeaths) / float64(stats.TotalGames)
	stats.AvgAssists = float64(totalAssists) / float64(stats.TotalGames)
	stats.AvgCS = float64(totalCS) / float64(stats.TotalGames)

	// Calculate KDA (kills + assists) / deaths, handle 0 deaths
	if totalDeaths > 0 {
		stats.AvgKDA = float64(totalKills+totalAssists) / float64(totalDeaths)
	} else {
		stats.AvgKDA = float64(totalKills + totalAssists)
	}

	// CS per minute
	avgGameMinutes := float64(totalGameDuration) / float64(stats.TotalGames) / 60.0
	if avgGameMinutes > 0 {
		stats.AvgCSPerMin = stats.AvgCS / avgGameMinutes
	}

	// Finalize champion stats
	for _, cd := range champData {
		if cd.Games > 0 {
			cd.WinRate = float64(cd.Wins) / float64(cd.Games) * 100
			cd.AvgKills = cd.AvgKills / float64(cd.Games)
			cd.AvgDeaths = cd.AvgDeaths / float64(cd.Games)
			cd.AvgAssists = cd.AvgAssists / float64(cd.Games)
			cd.AvgCS = cd.AvgCS / float64(cd.Games)
			if cd.AvgDeaths > 0 {
				cd.AvgKDA = (cd.AvgKills + cd.AvgAssists) / cd.AvgDeaths
			} else {
				cd.AvgKDA = cd.AvgKills + cd.AvgAssists
			}
			// Calculate CS per minute for this champion
			avgChampGameMinutes := float64(cd.TotalDuration) / float64(cd.Games) / 60.0
			if avgChampGameMinutes > 0 {
				cd.AvgCSPerMin = cd.AvgCS / avgChampGameMinutes
			}

			// Determine most common role
			maxCount := 0
			for role, count := range cd.RoleCounts {
				if count > maxCount {
					maxCount = count
					cd.Role = role
				}
			}
			if iconURL, ok := roleIconURLs[cd.Role]; ok {
				cd.RoleIconURL = iconURL
			}

			stats.ChampionStats = append(stats.ChampionStats, *cd)
		}
	}

	// Sort champion stats by games played (descending) and keep only the top one
	for i := 0; i < len(stats.ChampionStats); i++ {
		for j := i + 1; j < len(stats.ChampionStats); j++ {
			if stats.ChampionStats[j].Games > stats.ChampionStats[i].Games {
				stats.ChampionStats[i], stats.ChampionStats[j] = stats.ChampionStats[j], stats.ChampionStats[i]
			}
		}
	}

	// Keep top 5 champions
	if len(stats.ChampionStats) > 5 {
		stats.ChampionStats = stats.ChampionStats[:5]
	}

	return stats
}
