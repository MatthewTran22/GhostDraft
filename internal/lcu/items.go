package lcu

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ItemData holds item information
type ItemData struct {
	Name string `json:"name"`
	Gold struct {
		Total int `json:"total"`
	} `json:"gold"`
}

// ItemRegistry holds item ID to name mapping
type ItemRegistry struct {
	items   map[int]string
	mu      sync.RWMutex
	loaded  bool
	version string
}

// NewItemRegistry creates a new item registry
func NewItemRegistry() *ItemRegistry {
	return &ItemRegistry{
		items: make(map[int]string),
	}
}

// Load fetches item data from Data Dragon
func (r *ItemRegistry) Load() error {
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

	r.version = versions[0]

	// Get item data
	itemURL := fmt.Sprintf("https://ddragon.leagueoflegends.com/cdn/%s/data/en_US/item.json", r.version)
	itemResp, err := client.Get(itemURL)
	if err != nil {
		return fmt.Errorf("failed to fetch items: %w", err)
	}
	defer itemResp.Body.Close()

	var itemData struct {
		Data map[string]ItemData `json:"data"`
	}
	if err := json.NewDecoder(itemResp.Body).Decode(&itemData); err != nil {
		return fmt.Errorf("failed to parse items: %w", err)
	}

	// Build ID -> Name map
	for idStr, item := range itemData.Data {
		var id int
		fmt.Sscanf(idStr, "%d", &id)
		r.items[id] = item.Name
	}

	r.loaded = true
	fmt.Printf("Loaded %d items from Data Dragon (v%s)\n", len(r.items), r.version)
	return nil
}

// GetName returns the item name for a given ID
func (r *ItemRegistry) GetName(id int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name, ok := r.items[id]; ok {
		return name
	}
	return fmt.Sprintf("Item %d", id)
}

// GetVersion returns the loaded version
func (r *ItemRegistry) GetVersion() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// GetIconURL returns the Data Dragon icon URL for an item
func (r *ItemRegistry) GetIconURL(id int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return fmt.Sprintf("https://ddragon.leagueoflegends.com/cdn/%s/img/item/%d.png", r.version, id)
}
