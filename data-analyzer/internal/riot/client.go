package riot

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
)

const (
	// API base URLs
	americasBaseURL = "https://americas.api.riotgames.com"
	na1BaseURL      = "https://na1.api.riotgames.com" // Regional URL for summoner/league endpoints

	// Rate limit settings (conservative: 90 req/2min instead of 100)
	maxRequestsPer2Min = 90
	rateLimitWindow    = 2 * time.Minute
	minRequestInterval = 50 * time.Millisecond // Max ~20 req/sec
)

// Client is a Riot API client that handles 429 rate limit responses
type Client struct {
	apiKey       string
	httpClient   *http.Client
	requestTimes []time.Time
	mu           sync.Mutex
}

// NewClient creates a new Riot API client
func NewClient() (*Client, error) {
	apiKey := os.Getenv("RIOT_API_KEY")
	if apiKey == "" {
		// Also check alternative env var name
		apiKey = os.Getenv("RIOT-DEV-KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("RIOT_API_KEY or RIOT-DEV-KEY environment variable not set")
	}

	// Show key prefix for debugging (don't show full key)
	if len(apiKey) > 10 {
		fmt.Printf("Using API key: %s...%s\n", apiKey[:8], apiKey[len(apiKey)-4:])
	}

	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		requestTimes: make([]time.Time, 0, maxRequestsPer2Min),
	}, nil
}

// rateLimit implements a sliding window rate limiter
func (c *Client) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-rateLimitWindow)

	// Remove requests outside the 2-minute window
	validTimes := make([]time.Time, 0, len(c.requestTimes))
	for _, t := range c.requestTimes {
		if t.After(windowStart) {
			validTimes = append(validTimes, t)
		}
	}
	c.requestTimes = validTimes

	// If at limit, wait until oldest request expires
	if len(c.requestTimes) >= maxRequestsPer2Min {
		oldestRequest := c.requestTimes[0]
		waitUntil := oldestRequest.Add(rateLimitWindow)
		sleepDuration := time.Until(waitUntil)
		if sleepDuration > 0 {
			fmt.Printf("      [Rate Limit] At %d/%d requests, waiting %.1fs...\n",
				len(c.requestTimes), maxRequestsPer2Min, sleepDuration.Seconds())
			c.mu.Unlock()
			time.Sleep(sleepDuration + 100*time.Millisecond) // Small buffer
			c.mu.Lock()
			// Re-clean after sleeping
			now = time.Now()
			windowStart = now.Add(-rateLimitWindow)
			validTimes = make([]time.Time, 0, len(c.requestTimes))
			for _, t := range c.requestTimes {
				if t.After(windowStart) {
					validTimes = append(validTimes, t)
				}
			}
			c.requestTimes = validTimes
		}
	}

	// Enforce minimum interval between requests (20 req/sec max)
	if len(c.requestTimes) > 0 {
		lastRequest := c.requestTimes[len(c.requestTimes)-1]
		elapsed := time.Since(lastRequest)
		if elapsed < minRequestInterval {
			c.mu.Unlock()
			time.Sleep(minRequestInterval - elapsed)
			c.mu.Lock()
		}
	}

	// Record this request
	c.requestTimes = append(c.requestTimes, time.Now())
}

// doRequest makes a request and handles 429 rate limit responses
func (c *Client) doRequest(ctx context.Context, url string, result interface{}) error {
	// Smart rate limiting
	c.rateLimit()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Riot-Token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		// Rate limited - wait 30 seconds and retry
		fmt.Printf("      [429 Rate Limited] Waiting 30 seconds...\n")
		time.Sleep(30 * time.Second)
		return c.doRequest(ctx, url, result)
	}

	if resp.StatusCode == 403 {
		return fmt.Errorf("API returned 403 Forbidden - check if your API key is valid")
	}

	if resp.StatusCode == 404 {
		return fmt.Errorf("API returned 404 Not Found - player/match may not exist")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

// GetAccountByRiotID fetches account info by Riot ID (gameName#tagLine)
func (c *Client) GetAccountByRiotID(ctx context.Context, gameName, tagLine string) (*AccountResponse, error) {
	url := fmt.Sprintf("%s/riot/account/v1/accounts/by-riot-id/%s/%s",
		americasBaseURL, gameName, tagLine)

	var account AccountResponse
	err := c.doRequest(ctx, url, &account)
	return &account, err
}

// GetAccountByPUUID fetches account info by PUUID
func (c *Client) GetAccountByPUUID(ctx context.Context, puuid string) (*AccountResponse, error) {
	url := fmt.Sprintf("%s/riot/account/v1/accounts/by-puuid/%s",
		americasBaseURL, puuid)

	var account AccountResponse
	err := c.doRequest(ctx, url, &account)
	return &account, err
}

// GetMatchHistory fetches match IDs for a player
func (c *Client) GetMatchHistory(ctx context.Context, puuid string, count int) ([]string, error) {
	url := fmt.Sprintf("%s/lol/match/v5/matches/by-puuid/%s/ids?queue=420&count=%d",
		americasBaseURL, puuid, count)

	var matchIDs []string
	err := c.doRequest(ctx, url, &matchIDs)
	return matchIDs, err
}

// GetMatch fetches match details
func (c *Client) GetMatch(ctx context.Context, matchID string) (*MatchResponse, error) {
	url := fmt.Sprintf("%s/lol/match/v5/matches/%s", americasBaseURL, matchID)

	var match MatchResponse
	err := c.doRequest(ctx, url, &match)
	return &match, err
}

// GetTimeline fetches match timeline
func (c *Client) GetTimeline(ctx context.Context, matchID string) (*TimelineResponse, error) {
	url := fmt.Sprintf("%s/lol/match/v5/matches/%s/timeline", americasBaseURL, matchID)

	var timeline TimelineResponse
	err := c.doRequest(ctx, url, &timeline)
	return &timeline, err
}

// GetRankedEntriesByPUUID fetches ranked entries directly by PUUID
func (c *Client) GetRankedEntriesByPUUID(ctx context.Context, puuid string) ([]LeagueEntryResponse, error) {
	url := fmt.Sprintf("%s/lol/league/v4/entries/by-puuid/%s", na1BaseURL, puuid)

	var entries []LeagueEntryResponse
	err := c.doRequest(ctx, url, &entries)
	return entries, err
}

// GetSoloQueueRank fetches the solo queue rank for a player by PUUID
// Returns tier, division, and whether they have a solo queue rank
func (c *Client) GetSoloQueueRank(ctx context.Context, puuid string) (tier string, division string, hasRank bool, err error) {
	entries, err := c.GetRankedEntriesByPUUID(ctx, puuid)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to get ranked entries: %w", err)
	}

	// Find solo queue entry
	for _, entry := range entries {
		if entry.QueueType == "RANKED_SOLO_5x5" {
			return entry.Tier, entry.Rank, true, nil
		}
	}

	// No solo queue rank found
	return "", "", false, nil
}

// ChallengerLeagueResponse represents the challenger league
type ChallengerLeagueResponse struct {
	Tier    string                   `json:"tier"`
	LeagueID string                  `json:"leagueId"`
	Queue   string                   `json:"queue"`
	Entries []ChallengerLeagueEntry  `json:"entries"`
}

type ChallengerLeagueEntry struct {
	SummonerID   string `json:"summonerId"`
	PUUID        string `json:"puuid"`
	LeaguePoints int    `json:"leaguePoints"`
	Rank         string `json:"rank"`
	Wins         int    `json:"wins"`
	Losses       int    `json:"losses"`
}

// GetChallengerLeague fetches the challenger league for solo queue
func (c *Client) GetChallengerLeague(ctx context.Context) (*ChallengerLeagueResponse, error) {
	url := fmt.Sprintf("%s/lol/league/v4/challengerleagues/by-queue/RANKED_SOLO_5x5", na1BaseURL)

	var league ChallengerLeagueResponse
	err := c.doRequest(ctx, url, &league)
	return &league, err
}

// GetTopChallengerPUUID fetches the PUUID of a top challenger player
// Returns the highest LP player's PUUID
func (c *Client) GetTopChallengerPUUID(ctx context.Context) (string, error) {
	league, err := c.GetChallengerLeague(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get challenger league: %w", err)
	}

	if len(league.Entries) == 0 {
		return "", fmt.Errorf("no challenger players found")
	}

	// Find highest LP player
	topPlayer := league.Entries[0]
	for _, entry := range league.Entries[1:] {
		if entry.LeaguePoints > topPlayer.LeaguePoints {
			topPlayer = entry
		}
	}

	if topPlayer.PUUID == "" {
		return "", fmt.Errorf("top challenger player has no PUUID")
	}

	return topPlayer.PUUID, nil
}

// GetCurrentPatch fetches the current game patch from Data Dragon
func GetCurrentPatch(ctx context.Context) (string, error) {
	url := "https://ddragon.leagueoflegends.com/api/versions.json"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Data Dragon returned status %d", resp.StatusCode)
	}

	var versions []string
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return "", err
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no versions returned from Data Dragon")
	}

	// Return normalized patch (e.g., "14.24.1" -> "14.24")
	return NormalizePatch(versions[0]), nil
}

// NormalizePatch converts a full version string to major.minor format
// e.g., "14.24.448.1234" -> "14.24"
func NormalizePatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// ExtractBuildOrder extracts item purchase order for a participant from timeline
func ExtractBuildOrder(timeline *TimelineResponse, participantID int) []int {
	var buildOrder []int
	seenItems := make(map[int]bool)

	for _, frame := range timeline.Info.Frames {
		for _, event := range frame.Events {
			if event.Type == "ITEM_PURCHASED" && event.ParticipantID == participantID {
				// Only include completed items, skip duplicates
				if IsCompletedItem(event.ItemID) && !seenItems[event.ItemID] {
					buildOrder = append(buildOrder, event.ItemID)
					seenItems[event.ItemID] = true
				}
			}
		}
	}

	return buildOrder
}
