package ugg

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	patchesURL   = "https://static.bigbrain.gg/assets/lol/riot_patch_update/prod/ugg/patches.json"
	statsBaseURL = "https://stats2.u.gg/lol"
	apiVersion   = "1.5"
	statsVersion = "1.5.0"
)

// Fetcher handles U.GG data fetching
type Fetcher struct {
	client       *http.Client
	currentPatch string
	mu           sync.RWMutex
	cache        map[string]*BuildData
	matchupCache map[string][]MatchupData
}

// NewFetcher creates a new U.GG fetcher
func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache:        make(map[string]*BuildData),
		matchupCache: make(map[string][]MatchupData),
	}
}

// FetchPatch fetches the current patch version from U.GG
func (f *Fetcher) FetchPatch() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	req, err := http.NewRequest("GET", patchesURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch patches: %w", err)
	}
	defer resp.Body.Close()

	var patches []string
	if err := json.NewDecoder(resp.Body).Decode(&patches); err != nil {
		return fmt.Errorf("failed to parse patches: %w", err)
	}

	if len(patches) == 0 {
		return fmt.Errorf("no patches available")
	}

	f.currentPatch = patches[0]
	fmt.Printf("U.GG: Current patch is %s\n", f.currentPatch)
	return nil
}

// GetPatch returns the current patch
func (f *Fetcher) GetPatch() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.currentPatch
}

// ClearCache clears the cached data
func (f *Fetcher) ClearCache() {
	f.mu.Lock()
	f.cache = make(map[string]*BuildData)
	f.matchupCache = make(map[string][]MatchupData)
	f.mu.Unlock()
}

// roleToID converts role name to U.GG role ID
func roleToID(role string) string {
	roleMap := map[string]string{
		"top":     "4",
		"jungle":  "1",
		"middle":  "5",
		"bottom":  "3",
		"utility": "2",
		"":        "",
	}
	return roleMap[role]
}

// getWinsAndGames extracts wins and games from stats data
func (f *Fetcher) getWinsAndGames(data json.RawMessage) (float64, float64) {
	var stats []float64
	if err := json.Unmarshal(data, &stats); err != nil || len(stats) < 2 {
		return 0, 0
	}
	return stats[0], stats[1]
}
