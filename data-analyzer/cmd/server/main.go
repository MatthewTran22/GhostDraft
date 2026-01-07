package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	json "github.com/goccy/go-json"

	"data-analyzer/internal/db"

	"github.com/joho/godotenv"
)

var database *db.DB

func main() {
	// Load .env file - try multiple locations
	envPaths := []string{".env", "../.env", "../../.env", "data-analyzer/.env"}
	envLoaded := false
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			fmt.Printf("Loaded .env from: %s\n", path)
			envLoaded = true
			break
		}
	}
	if !envLoaded {
		log.Println("No .env file found, using environment variables")
	}

	ctx := context.Background()

	// Connect to database
	var err error
	database, err = db.New(ctx)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// API routes
	http.HandleFunc("/api/stats", handleStats)
	http.HandleFunc("/api/matches", handleMatches)
	http.HandleFunc("/api/match/", handleMatchDetail)
	http.HandleFunc("/api/champions", handleChampions)

	// Aggregated stats routes (from reducer)
	http.HandleFunc("/api/aggregated/overview", handleAggregatedOverview)
	http.HandleFunc("/api/aggregated/patches", handlePatches)
	http.HandleFunc("/api/aggregated/champions", handleAggregatedChampions)
	http.HandleFunc("/api/aggregated/items", handleAggregatedItems)
	http.HandleFunc("/api/aggregated/matchups", handleAggregatedMatchups)

	// Static files - try multiple paths
	webDir := "web"
	webPaths := []string{"web", "../web", "../../web", "data-analyzer/web"}
	for _, p := range webPaths {
		if _, err := os.Stat(p); err == nil {
			webDir = p
			break
		}
	}
	fmt.Printf("Serving static files from: %s\n", webDir)
	http.Handle("/", http.FileServer(http.Dir(webDir)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server starting on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	matchCount, err := database.GetMatchCount(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	participantCount, err := database.GetParticipantCount(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"matches":      matchCount,
		"participants": participantCount,
	})
}

func handleMatches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	matches, err := database.GetRecentMatches(ctx, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(matches)
}

func handleChampions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := database.GetChampionStats(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleMatchDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract match ID from URL path: /api/match/{matchId}
	matchID := strings.TrimPrefix(r.URL.Path, "/api/match/")
	if matchID == "" {
		http.Error(w, "Match ID required", http.StatusBadRequest)
		return
	}

	match, err := database.GetMatchDetail(ctx, matchID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(match)
}

func handleAggregatedOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	overview, err := database.GetStatsOverview(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(overview)
}

func handlePatches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	patches, err := database.GetPatches(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(patches)
}

func handleAggregatedChampions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	patch := r.URL.Query().Get("patch")

	stats, err := database.GetAggregatedChampionStats(ctx, patch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleAggregatedItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	patch := r.URL.Query().Get("patch")
	championID := r.URL.Query().Get("champion")
	position := r.URL.Query().Get("position")

	if patch == "" || championID == "" || position == "" {
		http.Error(w, "patch, champion, and position query params required", http.StatusBadRequest)
		return
	}

	var champID int
	fmt.Sscanf(championID, "%d", &champID)

	stats, err := database.GetAggregatedItemStats(ctx, patch, champID, position)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleAggregatedMatchups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	patch := r.URL.Query().Get("patch")
	championID := r.URL.Query().Get("champion")
	position := r.URL.Query().Get("position")
	limitStr := r.URL.Query().Get("limit")

	if patch == "" || championID == "" || position == "" {
		http.Error(w, "patch, champion, and position query params required", http.StatusBadRequest)
		return
	}

	var champID int
	fmt.Sscanf(championID, "%d", &champID)

	limit := 10
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	best, worst, err := database.GetAggregatedMatchupStats(ctx, patch, champID, position, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"best":  best,
		"worst": worst,
	})
}
