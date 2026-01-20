package storage

import (
	"bufio"
	"compress/gzip"
	json "github.com/goccy/go-json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// Rotation triggers
	MaxMatchesPerFile = 1000
	MaxFileAge        = 1 * time.Hour
)

// FileRotator handles writing matches to rotating JSONL files
type FileRotator struct {
	mu sync.Mutex

	// Directories
	hotDir  string // Active writes
	warmDir string // Closed files awaiting processing
	coldDir string // Compressed archives (set later)

	// Current file state
	currentFile   *os.File
	currentWriter *bufio.Writer
	currentPath   string
	matchCount    int
	fileOpenedAt  time.Time
}

// NewFileRotator creates a new rotator with the given base directory
func NewFileRotator(baseDir string) (*FileRotator, error) {
	hotDir := filepath.Join(baseDir, "hot")
	warmDir := filepath.Join(baseDir, "warm")
	coldDir := filepath.Join(baseDir, "cold")

	// Create directories
	for _, dir := range []string{hotDir, warmDir, coldDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	r := &FileRotator{
		hotDir:  hotDir,
		warmDir: warmDir,
		coldDir: coldDir,
	}

	// Open initial file
	if err := r.rotate(); err != nil {
		return nil, err
	}

	return r, nil
}

// SetColdDir allows setting a different cold storage path (e.g., HDD)
func (r *FileRotator) SetColdDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create cold directory: %w", err)
	}
	r.mu.Lock()
	r.coldDir = path
	r.mu.Unlock()
	return nil
}

// WriteLine writes a participant record to the current JSONL file
func (r *FileRotator) WriteLine(record interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Marshal record to JSON
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Write JSON line
	if _, err := r.currentWriter.Write(data); err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}
	if _, err := r.currentWriter.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// MatchComplete signals that a match has been fully written (all 10 participants)
// This increments the match counter and triggers rotation if needed
func (r *FileRotator) MatchComplete() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.matchCount++

	// Flush after each match
	if err := r.currentWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	// Check if rotation needed
	if r.shouldRotate() {
		if err := r.rotate(); err != nil {
			return err
		}
	}

	return nil
}

// shouldRotate checks if we need to rotate to a new file
func (r *FileRotator) shouldRotate() bool {
	if r.currentFile == nil {
		return true
	}
	if r.matchCount >= MaxMatchesPerFile {
		return true
	}
	if time.Since(r.fileOpenedAt) >= MaxFileAge {
		return true
	}
	return false
}

// rotate closes current file and opens a new one
func (r *FileRotator) rotate() error {
	// Close current file if open
	if r.currentFile != nil {
		if err := r.currentWriter.Flush(); err != nil {
			return fmt.Errorf("failed to flush before rotation: %w", err)
		}
		if err := r.currentFile.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}

		// Move to warm storage
		warmPath := filepath.Join(r.warmDir, filepath.Base(r.currentPath))
		if err := os.Rename(r.currentPath, warmPath); err != nil {
			return fmt.Errorf("failed to move to warm storage: %w", err)
		}
		fmt.Printf("[Rotator] Moved %s to warm storage (%d matches)\n", filepath.Base(r.currentPath), r.matchCount)
	}

	// Generate new filename
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("raw_matches_%s.jsonl", timestamp)
	r.currentPath = filepath.Join(r.hotDir, filename)

	// Open new file
	file, err := os.Create(r.currentPath)
	if err != nil {
		return fmt.Errorf("failed to create new file: %w", err)
	}

	r.currentFile = file
	r.currentWriter = bufio.NewWriterSize(file, 64*1024) // 64KB buffer
	r.matchCount = 0
	r.fileOpenedAt = time.Now()

	fmt.Printf("[Rotator] Opened new file: %s\n", filename)
	return nil
}

// Close flushes and closes the current file
func (r *FileRotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.currentFile == nil {
		return nil
	}

	if err := r.currentWriter.Flush(); err != nil {
		return err
	}

	if err := r.currentFile.Close(); err != nil {
		return err
	}

	// Move to warm if it has data
	if r.matchCount > 0 {
		warmPath := filepath.Join(r.warmDir, filepath.Base(r.currentPath))
		if err := os.Rename(r.currentPath, warmPath); err != nil {
			return err
		}
		fmt.Printf("[Rotator] Closed and moved %s to warm (%d matches)\n", filepath.Base(r.currentPath), r.matchCount)
	} else {
		// Remove empty file
		os.Remove(r.currentPath)
	}

	r.currentFile = nil
	return nil
}

// Stats returns current rotator statistics
func (r *FileRotator) Stats() (matchesInCurrentFile int, currentFileName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.matchCount, filepath.Base(r.currentPath)
}

// FlushAndRotate explicitly flushes the current hot file to warm directory and opens a new file.
// This is called by the reducer to ensure all data is available for processing.
// Returns true if a file was rotated (had data), false if the hot file was empty.
// Thread-safe: uses internal mutex to synchronize with writes.
func (r *FileRotator) FlushAndRotate() (rotated bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If no file is open, nothing to flush
	if r.currentFile == nil {
		return false, nil
	}

	// Flush the buffer first
	if err := r.currentWriter.Flush(); err != nil {
		return false, fmt.Errorf("failed to flush buffer: %w", err)
	}

	// If the file has no data (no matches completed), don't move an empty file
	if r.matchCount == 0 {
		// Check if file has any data at all (could have partial writes)
		info, err := r.currentFile.Stat()
		if err != nil {
			return false, fmt.Errorf("failed to stat current file: %w", err)
		}
		if info.Size() == 0 {
			// Truly empty file - don't rotate, just keep using it
			return false, nil
		}
	}

	// Close current file
	if err := r.currentFile.Close(); err != nil {
		return false, fmt.Errorf("failed to close file: %w", err)
	}

	// Move to warm storage
	warmPath := filepath.Join(r.warmDir, filepath.Base(r.currentPath))
	if err := os.Rename(r.currentPath, warmPath); err != nil {
		return false, fmt.Errorf("failed to move to warm storage: %w", err)
	}
	fmt.Printf("[Rotator] FlushAndRotate: moved %s to warm storage (%d matches)\n", filepath.Base(r.currentPath), r.matchCount)

	// Open new file
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("raw_matches_%s.jsonl", timestamp)
	r.currentPath = filepath.Join(r.hotDir, filename)

	file, err := os.Create(r.currentPath)
	if err != nil {
		return false, fmt.Errorf("failed to create new file: %w", err)
	}

	r.currentFile = file
	r.currentWriter = bufio.NewWriterSize(file, 64*1024) // 64KB buffer
	r.matchCount = 0
	r.fileOpenedAt = time.Now()

	fmt.Printf("[Rotator] FlushAndRotate: opened new file: %s\n", filename)
	return true, nil
}

// CompressToCold compresses a warm file and moves it to cold storage
func CompressToCold(warmPath, coldDir string) error {
	// Open source file
	src, err := os.Open(warmPath)
	if err != nil {
		return err
	}

	// Create compressed file
	filename := filepath.Base(warmPath) + ".gz"
	coldPath := filepath.Join(coldDir, filename)
	dst, err := os.Create(coldPath)
	if err != nil {
		src.Close()
		return err
	}

	// Compress
	gzWriter := gzip.NewWriter(dst)
	if _, err := io.Copy(gzWriter, src); err != nil {
		src.Close()
		dst.Close()
		return err
	}
	if err := gzWriter.Close(); err != nil {
		src.Close()
		dst.Close()
		return err
	}

	// Close files before removing (required on Windows)
	if err := src.Close(); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}

	// Remove original
	if err := os.Remove(warmPath); err != nil {
		return err
	}

	fmt.Printf("[Rotator] Compressed %s to cold storage\n", filepath.Base(warmPath))
	return nil
}
