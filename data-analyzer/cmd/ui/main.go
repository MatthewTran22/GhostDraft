package main

import (
	"bufio"
	"compress/gzip"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	json "github.com/goccy/go-json"
)

//go:embed templates/*
var templates embed.FS

var (
	pipelineRunning bool
	pipelineMu      sync.Mutex
	outputClients   = make(map[chan string]bool)
	outputMu        sync.Mutex
)

type PageData struct {
	RiotAPIKey  string
	StoragePath string
	TursoURL    string
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/start", handleStart)
	http.HandleFunc("/status", handleStatus)
	http.HandleFunc("/stream", handleStream)
	http.HandleFunc("/restore", handleRestore)

	fmt.Printf("Pipeline UI running at http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templates, "templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	data := PageData{
		RiotAPIKey:  maskKey(os.Getenv("RIOT_API_KEY")),
		StoragePath: os.Getenv("BLOB_STORAGE_PATH"),
		TursoURL:    maskTursoURL(os.Getenv("TURSO_DATABASE_URL")),
	}

	tmpl.Execute(w, data)
}

func maskKey(key string) string {
	if len(key) < 10 {
		return "Not set"
	}
	return key[:10] + "..." + key[len(key)-4:]
}

func maskTursoURL(url string) string {
	if url == "" {
		return "Not set"
	}
	// Show just the database name from libsql://db-name.turso.io
	if strings.HasPrefix(url, "libsql://") {
		parts := strings.Split(url[9:], ".")
		if len(parts) > 0 {
			return parts[0] + ".turso.io"
		}
	}
	return "Configured"
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	pipelineMu.Lock()
	running := pipelineRunning
	pipelineMu.Unlock()

	json.NewEncoder(w).Encode(map[string]bool{"running": running})
}

func handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	pipelineMu.Lock()
	if pipelineRunning {
		pipelineMu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"error": "Pipeline already running"})
		return
	}
	pipelineRunning = true
	pipelineMu.Unlock()

	riotID := r.FormValue("riot_id")
	matchCount := r.FormValue("match_count")
	maxPlayers := r.FormValue("max_players")
	reduceOnly := r.FormValue("reduce_only") == "true"

	// riot_id is optional - collector will auto-seed from Challenger if not provided

	go runPipeline(riotID, matchCount, maxPlayers, reduceOnly)

	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func runPipeline(riotID, matchCount, maxPlayers string, reduceOnly bool) {
	defer func() {
		pipelineMu.Lock()
		pipelineRunning = false
		pipelineMu.Unlock()
		broadcast("\n[PIPELINE COMPLETE]")
	}()

	// Find data-analyzer directory
	analyzerDir := findAnalyzerDir()
	if analyzerDir == "" {
		broadcast("[ERROR] Could not find data-analyzer directory")
		return
	}

	broadcast(fmt.Sprintf("[INFO] Working directory: %s", analyzerDir))

	if !reduceOnly {
		// Run collector
		broadcast("\n========================================")
		broadcast("STEP 1: COLLECTING MATCH DATA")
		broadcast("========================================")

		args := []string{
			"run", "./cmd/collector",
			"--count=" + matchCount,
			"--max-players=" + maxPlayers,
		}

		// Only add --riot-id if explicitly provided (otherwise collector auto-seeds from Challenger)
		if riotID != "" {
			args = append(args, "--riot-id="+riotID)
		} else {
			broadcast("[INFO] No Riot ID provided, auto-seeding from Challenger leaderboard")
		}

		if err := runCommandWithOutput(analyzerDir, "go", args...); err != nil {
			broadcast(fmt.Sprintf("[ERROR] Collector failed: %v", err))
			return
		}
	}

	// Run reducer
	broadcast("\n========================================")
	broadcast("STEP 2: REDUCING & EXPORTING DATA")
	broadcast("========================================\n")

	reducerArgs := []string{
		"run", "./cmd/reducer",
		"--output-dir=./export",
	}

	if err := runCommandWithOutput(analyzerDir, "go", reducerArgs...); err != nil {
		broadcast(fmt.Sprintf("[ERROR] Reducer failed: %v", err))
		return
	}

	broadcast("\n========================================")
	broadcast("SUCCESS!")
	broadcast("========================================")
	broadcast("Output: ./export/data.json")
	broadcast("Turso: Data pushed to cloud database")
}

func runCommandWithOutput(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	// Get pipes for stdout and stderr
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	broadcast(fmt.Sprintf("[CMD] %s %s", name, strings.Join(args, " ")))

	if err := cmd.Start(); err != nil {
		return err
	}

	// Stream stdout
	go streamOutput(stdout)
	go streamOutput(stderr)

	return cmd.Wait()
}

func streamOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		broadcast(scanner.Text())
	}
}

func broadcast(msg string) {
	outputMu.Lock()
	defer outputMu.Unlock()
	for ch := range outputClients {
		select {
		case ch <- msg:
		default:
			// Client too slow, skip
		}
	}
	// Also log to console
	fmt.Println(msg)
}

func handleStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", 500)
		return
	}

	// Create client channel
	ch := make(chan string, 100)
	outputMu.Lock()
	outputClients[ch] = true
	outputMu.Unlock()

	defer func() {
		outputMu.Lock()
		delete(outputClients, ch)
		outputMu.Unlock()
		close(ch)
	}()

	// Send initial connection message
	fmt.Fprintf(w, "data: [CONNECTED] Ready to start pipeline\n\n")
	flusher.Flush()

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func findAnalyzerDir() string {
	candidates := []string{
		".",
		"/app",
		"data-analyzer",
		"../data-analyzer",
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

func handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	target := r.URL.Query().Get("target") // "turso" or "github"
	if target != "turso" && target != "github" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid target, must be 'turso' or 'github'"})
		return
	}

	patchFilter := r.URL.Query().Get("patch") // e.g. "15.24" or "16.1"

	pipelineMu.Lock()
	if pipelineRunning {
		pipelineMu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"error": "Pipeline is running, cannot restore"})
		return
	}
	pipelineRunning = true
	pipelineMu.Unlock()

	storagePath := os.Getenv("BLOB_STORAGE_PATH")
	if storagePath == "" {
		pipelineMu.Lock()
		pipelineRunning = false
		pipelineMu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"error": "BLOB_STORAGE_PATH not set"})
		return
	}
	storagePath = strings.Trim(storagePath, "\"")

	coldDir := filepath.Join(storagePath, "cold")
	warmDir := filepath.Join(storagePath, "warm")

	// Find all .gz files in cold
	files, err := filepath.Glob(filepath.Join(coldDir, "*.jsonl.gz"))
	if err != nil {
		pipelineMu.Lock()
		pipelineRunning = false
		pipelineMu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to list cold files: %v", err)})
		return
	}

	if len(files) == 0 {
		pipelineMu.Lock()
		pipelineRunning = false
		pipelineMu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"error": "No files in cold storage to restore"})
		return
	}

	// Run restore and reducer in background
	go runRestoreAndReduce(files, coldDir, warmDir, target, patchFilter)

	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func runRestoreAndReduce(files []string, coldDir, warmDir, target, patchFilter string) {
	defer func() {
		pipelineMu.Lock()
		pipelineRunning = false
		pipelineMu.Unlock()
		broadcast("\n[PIPELINE COMPLETE]")
	}()

	// Step 1: Restore files from cold to warm
	broadcast("\n========================================")
	broadcast("STEP 1: RESTORING FROM COLD STORAGE")
	if patchFilter != "" {
		broadcast(fmt.Sprintf("         (filtering for patch %s)", patchFilter))
	}
	broadcast("========================================\n")

	totalRecords := 0
	filteredRecords := 0
	for _, coldPath := range files {
		total, filtered, err := decompressToWarmFiltered(coldPath, warmDir, patchFilter)
		if err != nil {
			broadcast(fmt.Sprintf("[ERROR] Failed to restore %s: %v", filepath.Base(coldPath), err))
			continue
		}
		totalRecords += total
		filteredRecords += filtered
		if patchFilter != "" {
			broadcast(fmt.Sprintf("[RESTORE] %s: %d/%d records matched patch %s", filepath.Base(coldPath), filtered, total, patchFilter))
		} else {
			broadcast(fmt.Sprintf("[RESTORE] Restored %s (%d records)", filepath.Base(coldPath), total))
		}
	}

	if patchFilter != "" {
		broadcast(fmt.Sprintf("[RESTORE] Total: %d/%d records matched patch %s", filteredRecords, totalRecords, patchFilter))
	} else {
		broadcast(fmt.Sprintf("[RESTORE] Total: %d records restored", totalRecords))
	}

	if filteredRecords == 0 && patchFilter != "" {
		broadcast("[ERROR] No records matched the patch filter, skipping reducer")
		return
	}
	if totalRecords == 0 {
		broadcast("[ERROR] No records were restored, skipping reducer")
		return
	}

	// Step 2: Run reducer with appropriate flags
	broadcast("\n========================================")
	if target == "turso" {
		broadcast("STEP 2: PUSHING TO TURSO")
	} else {
		broadcast("STEP 2: PUSHING TO GITHUB")
	}
	broadcast("========================================\n")

	analyzerDir := findAnalyzerDir()
	if analyzerDir == "" {
		broadcast("[ERROR] Could not find data-analyzer directory")
		return
	}

	reducerArgs := []string{
		"run", "./cmd/reducer",
		"--output-dir=./export",
	}

	if target == "turso" {
		reducerArgs = append(reducerArgs, "--skip-release")
	} else {
		reducerArgs = append(reducerArgs, "--skip-turso")
	}

	if err := runCommandWithOutput(analyzerDir, "go", reducerArgs...); err != nil {
		broadcast(fmt.Sprintf("[ERROR] Reducer failed: %v", err))
		return
	}

	broadcast("\n========================================")
	broadcast("SUCCESS!")
	broadcast("========================================")
}

// decompressToWarmFiltered decompresses a cold file to warm, optionally filtering by patch.
// Returns (totalRecords, filteredRecords, error).
func decompressToWarmFiltered(coldPath, warmDir, patchFilter string) (int, int, error) {
	// Open compressed file
	src, err := os.Open(coldPath)
	if err != nil {
		return 0, 0, err
	}
	defer src.Close()

	gzReader, err := gzip.NewReader(src)
	if err != nil {
		return 0, 0, err
	}
	defer gzReader.Close()

	// Create warm file (remove .gz extension)
	filename := filepath.Base(coldPath)
	filename = strings.TrimSuffix(filename, ".gz")
	warmPath := filepath.Join(warmDir, filename)

	dst, err := os.Create(warmPath)
	if err != nil {
		return 0, 0, err
	}
	defer dst.Close()

	writer := bufio.NewWriter(dst)
	scanner := bufio.NewScanner(gzReader)
	// Increase scanner buffer for large lines
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	totalRecords := 0
	filteredRecords := 0

	for scanner.Scan() {
		line := scanner.Text()
		totalRecords++

		// If no filter, write all lines
		if patchFilter == "" {
			writer.WriteString(line)
			writer.WriteString("\n")
			filteredRecords++
			continue
		}

		// Parse the line to check gameVersion
		var record struct {
			GameVersion string `json:"gameVersion"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			// If we can't parse, skip the line
			continue
		}

		// Normalize the gameVersion (e.g., "15.24.734" -> "15.24")
		normalizedPatch := normalizePatch(record.GameVersion)
		if normalizedPatch == patchFilter {
			writer.WriteString(line)
			writer.WriteString("\n")
			filteredRecords++
		}
	}

	if err := scanner.Err(); err != nil {
		os.Remove(warmPath)
		return 0, 0, err
	}

	if err := writer.Flush(); err != nil {
		os.Remove(warmPath)
		return 0, 0, err
	}

	// If filtering and no records matched, remove the empty file
	if patchFilter != "" && filteredRecords == 0 {
		os.Remove(warmPath)
	}

	// Remove the cold file after successful restore
	if err := os.Remove(coldPath); err != nil {
		return totalRecords, filteredRecords, fmt.Errorf("restored but failed to remove cold file: %w", err)
	}

	return totalRecords, filteredRecords, nil
}

// normalizePatch converts "15.24.734" to "15.24"
func normalizePatch(gameVersion string) string {
	parts := strings.Split(gameVersion, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return gameVersion
}
