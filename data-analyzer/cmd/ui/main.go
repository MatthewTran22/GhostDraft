package main

import (
	"bufio"
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

	if riotID == "" && !reduceOnly {
		pipelineMu.Lock()
		pipelineRunning = false
		pipelineMu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"error": "Riot ID is required"})
		return
	}

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
		broadcast("========================================\n")

		args := []string{
			"run", "./cmd/collector",
			"--riot-id=" + riotID,
			"--count=" + matchCount,
			"--max-players=" + maxPlayers,
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
