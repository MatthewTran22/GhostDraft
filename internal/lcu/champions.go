package lcu

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ChampionData holds champion information
type ChampionData struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ChampionInfo holds champion name and icon ID
type ChampionInfo struct {
	Name   string // Display name (e.g., "Ahri")
	IconID string // Icon ID for Data Dragon (e.g., "Ahri")
}

// ChampionRegistry holds the champion ID to name mapping
type ChampionRegistry struct {
	champions map[int]ChampionInfo // key -> info (key is the numeric ID)
	version   string
	mu        sync.RWMutex
	loaded    bool
}

// NewChampionRegistry creates a new champion registry
func NewChampionRegistry() *ChampionRegistry {
	return &ChampionRegistry{
		champions: make(map[int]ChampionInfo),
	}
}

// Load fetches champion data from Data Dragon
func (r *ChampionRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	client := &http.Client{Timeout: 10 * time.Second}

	// Get latest version
	versionsResp, err := client.Get("https://ddragon.leagueoflegends.com/api/versions.json")
	if err != nil {
		return fmt.Errorf("failed to fetch versions: %w", err)
	}
	defer versionsResp.Body.Close()

	var versions []string
	if err := json.NewDecoder(versionsResp.Body).Decode(&versions); err != nil {
		return fmt.Errorf("failed to parse versions: %w", err)
	}

	if len(versions) == 0 {
		return fmt.Errorf("no versions available")
	}

	latestVersion := versions[0]

	// Get champion data
	champURL := fmt.Sprintf("https://ddragon.leagueoflegends.com/cdn/%s/data/en_US/champion.json", latestVersion)
	champResp, err := client.Get(champURL)
	if err != nil {
		return fmt.Errorf("failed to fetch champions: %w", err)
	}
	defer champResp.Body.Close()

	var champData struct {
		Data map[string]ChampionData `json:"data"`
	}
	if err := json.NewDecoder(champResp.Body).Decode(&champData); err != nil {
		return fmt.Errorf("failed to parse champions: %w", err)
	}

	// Build ID -> ChampionInfo map
	for id, champ := range champData.Data {
		key, err := strconv.Atoi(champ.Key)
		if err != nil {
			continue
		}
		r.champions[key] = ChampionInfo{
			Name:   champ.Name,
			IconID: id, // The map key is the icon ID (e.g., "Ahri", "MonkeyKing")
		}
	}

	r.version = latestVersion
	r.loaded = true
	fmt.Printf("Loaded %d champions from Data Dragon (v%s)\n", len(r.champions), latestVersion)

	// Debug: Check for new champions
	for _, checkID := range []int{799, 800, 904} {
		if info, ok := r.champions[checkID]; ok {
			fmt.Printf("  ✓ Champion %d: %s (icon: %s)\n", checkID, info.Name, info.IconID)
		} else {
			fmt.Printf("  ✗ Champion %d: NOT FOUND\n", checkID)
		}
	}

	return nil
}

// GetName returns the champion name for a given ID
func (r *ChampionRegistry) GetName(id int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if info, ok := r.champions[id]; ok {
		return info.Name
	}
	return fmt.Sprintf("Champion %d", id)
}

// GetIconURL returns the Data Dragon icon URL for a given champion ID
func (r *ChampionRegistry) GetIconURL(id int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if info, ok := r.champions[id]; ok {
		return fmt.Sprintf("https://ddragon.leagueoflegends.com/cdn/%s/img/champion/%s.png", r.version, info.IconID)
	}
	return ""
}

// GetSplashURL returns the Data Dragon splash art URL for a given champion ID
func (r *ChampionRegistry) GetSplashURL(id int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if info, ok := r.champions[id]; ok {
		return fmt.Sprintf("https://ddragon.leagueoflegends.com/cdn/img/champion/splash/%s_0.jpg", info.IconID)
	}
	return ""
}

// IsLoaded returns whether the registry has been loaded
func (r *ChampionRegistry) IsLoaded() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loaded
}
