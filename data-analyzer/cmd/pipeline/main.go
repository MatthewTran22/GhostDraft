package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// Flags
	riotID := flag.String("riot-id", "", "Starting Riot ID (e.g., 'Player#NA1')")
	matchCount := flag.Int("count", 20, "Number of matches to fetch per player")
	maxPlayers := flag.Int("max-players", 100, "Maximum unique players to collect")
	outputDir := flag.String("output-dir", "./export", "Directory for reducer output")
	skipCollector := flag.Bool("reduce-only", false, "Skip collector, only run reducer")
	flag.Parse()

	// Load .env
	envPaths := []string{".env", "../.env", "../../.env", "data-analyzer/.env"}
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			fmt.Printf("Loaded .env from: %s\n", path)
			break
		}
	}

	// Find the data-analyzer directory
	analyzerDir := findAnalyzerDir()
	if analyzerDir == "" {
		log.Fatal("Could not find data-analyzer directory")
	}
	fmt.Printf("Working directory: %s\n", analyzerDir)

	startTime := time.Now()

	// Step 1: Run collector (unless skip flag set)
	if !*skipCollector {
		fmt.Println("\n========================================")
		fmt.Println("STEP 1: COLLECTING MATCH DATA")
		fmt.Println("========================================")

		collectorArgs := []string{
			"run", "./cmd/collector",
			fmt.Sprintf("--count=%d", *matchCount),
			fmt.Sprintf("--max-players=%d", *maxPlayers),
		}

		// Only add --riot-id if explicitly provided (otherwise collector auto-seeds from Challenger)
		if *riotID != "" {
			collectorArgs = append(collectorArgs, "--riot-id="+*riotID)
		} else {
			fmt.Println("No --riot-id provided, collector will auto-seed from Challenger leaderboard")
		}

		if err := runCommand(analyzerDir, "go", collectorArgs...); err != nil {
			log.Fatalf("Collector failed: %v", err)
		}

		fmt.Printf("\nCollection completed in %s\n", time.Since(startTime).Round(time.Second))
	}

	// Step 2: Run reducer
	fmt.Println("\n========================================")
	fmt.Println("STEP 2: REDUCING & EXPORTING DATA")
	fmt.Println("========================================")

	reducerArgs := []string{
		"run", "./cmd/reducer",
		"--output-dir=" + *outputDir,
	}

	if err := runCommand(analyzerDir, "go", reducerArgs...); err != nil {
		log.Fatalf("Reducer failed: %v", err)
	}

	totalTime := time.Since(startTime).Round(time.Second)

	fmt.Println("\n========================================")
	fmt.Println("PIPELINE COMPLETE")
	fmt.Println("========================================")
	fmt.Printf("Total time: %s\n", totalTime)
	fmt.Printf("Output: %s/data.json\n", *outputDir)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Upload data.json to your CDN/GitHub")
	fmt.Println("  2. Update manifest.json with new version")
	fmt.Println("  3. Restart the app or call ForceStatsUpdate()")
}

func runCommand(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Printf("Running: %s %s\n\n", name, strings.Join(args, " "))
	return cmd.Run()
}

func findAnalyzerDir() string {
	// Try common locations
	candidates := []string{
		".",
		"data-analyzer",
		"../data-analyzer",
		"../../data-analyzer",
	}

	for _, candidate := range candidates {
		path := filepath.Join(candidate, "cmd", "collector", "main.go")
		if _, err := os.Stat(path); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
	}

	return ""
}
