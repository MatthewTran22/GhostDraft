package lcu

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var (
	ErrLockfileNotFound = errors.New("lockfile not found")
	ErrLeagueNotRunning = errors.New("league client is not running")
)

// Credentials holds the LCU connection details parsed from lockfile
type Credentials struct {
	ProcessName string
	PID         string
	Port        string
	Password    string
	Protocol    string
}

// Client represents a connection to the League Client
type Client struct {
	credentials *Credentials
	httpClient  *http.Client
	wsConn      *websocket.Conn
	baseURL     string
	authHeader  string
}

// NewClient creates a new LCU client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // LCU uses self-signed cert
				},
			},
			Timeout: 2 * time.Second, // Short timeout for quick disconnect detection
		},
	}
}

// FindLockfile searches for the League Client lockfile
func FindLockfile() (string, error) {
	// Common League installation paths on Windows
	possiblePaths := []string{
		"C:/Riot Games/League of Legends/lockfile",
		"D:/Riot Games/League of Legends/lockfile",
		"C:/Program Files/Riot Games/League of Legends/lockfile",
		"C:/Program Files (x86)/Riot Games/League of Legends/lockfile",
	}

	// Also check running processes for the actual install location
	// by looking at common alternative drives
	for _, drive := range []string{"E:", "F:", "G:"} {
		possiblePaths = append(possiblePaths, filepath.Join(drive, "Riot Games/League of Legends/lockfile"))
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", ErrLockfileNotFound
}

// ParseLockfile reads and parses the lockfile content
func ParseLockfile(path string) (*Credentials, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read lockfile: %w", err)
	}

	// Lockfile format: LeagueClient:pid:port:password:protocol
	parts := strings.Split(string(content), ":")
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid lockfile format: expected 5 parts, got %d", len(parts))
	}

	return &Credentials{
		ProcessName: parts[0],
		PID:         parts[1],
		Port:        parts[2],
		Password:    parts[3],
		Protocol:    parts[4],
	}, nil
}

// Connect establishes connection to the League Client
func (c *Client) Connect() error {
	lockfilePath, err := FindLockfile()
	if err != nil {
		return err
	}

	creds, err := ParseLockfile(lockfilePath)
	if err != nil {
		return err
	}

	c.credentials = creds
	c.baseURL = fmt.Sprintf("https://127.0.0.1:%s", creds.Port)
	c.authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte("riot:"+creds.Password))

	// Test connection
	if err := c.testConnection(); err != nil {
		return fmt.Errorf("failed to connect to LCU: %w", err)
	}

	return nil
}

// testConnection verifies we can reach the LCU API
func (c *Client) testConnection() error {
	req, err := http.NewRequest("GET", c.baseURL+"/lol-summoner/v1/current-summoner", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// IsConnected checks if the client is still connected to LCU
// by making a health check request
func (c *Client) IsConnected() bool {
	if c.credentials == nil {
		return false
	}

	// Verify connection is still alive
	if err := c.testConnection(); err != nil {
		// Connection lost, clear credentials
		c.credentials = nil
		return false
	}

	return true
}

// GetCredentials returns the current LCU credentials
func (c *Client) GetCredentials() *Credentials {
	return c.credentials
}

// GetPort returns the LCU port
func (c *Client) GetPort() string {
	if c.credentials == nil {
		return ""
	}
	return c.credentials.Port
}

// Disconnect cleans up the client connection
func (c *Client) Disconnect() {
	if c.wsConn != nil {
		c.wsConn.Close()
		c.wsConn = nil
	}
	c.credentials = nil
}

// Get performs a GET request to the LCU API
func (c *Client) Get(endpoint string) (*http.Response, error) {
	if c.credentials == nil {
		return nil, ErrLeagueNotRunning
	}

	req, err := http.NewRequest("GET", c.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authHeader)
	return c.httpClient.Do(req)
}
