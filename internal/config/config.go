package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config holds all user-adjustable settings, persisted as JSON in %APPDATA%\Quarterlog\config.json.
type Config struct {
	// MiniMax vision API.
	MiniMaxAPIKey  string `json:"miniMaxApiKey"`
	MiniMaxBaseURL string `json:"miniMaxBaseUrl"`
	MiniMaxModel   string `json:"miniMaxModel"`

	// Local Excel worklog file.
	FilePath string `json:"filePath"`

	// Worklog list fields.
	Categories string `json:"categories"` // newline-separated "Category from order" options
	Types      string `json:"types"`      // newline-separated "Type" options

	// Behaviour.
	IntervalMinutes int    `json:"intervalMinutes"` // logging cadence, default 15
	Monitor         int    `json:"monitor"`         // -1 = primary, -2 = all stitched, >=0 = specific display
	PopupPosition   string `json:"popupPosition"`   // where the popup appears, e.g. "bottom-right", "center"
	Language        string `json:"language"`        // language the AI writes descriptions in
	Prompt          string `json:"prompt"`          // extra guidance appended to the AI system prompt
	Paused          bool   `json:"paused"`
	Autostart       bool   `json:"autostart"`
}

// DefaultWorklogPath returns <UserHome>\Documents\Quarterlog\worklog.xlsx.
func DefaultWorklogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "worklog.xlsx"
	}
	return filepath.Join(home, "Documents", "Quarterlog", "worklog.xlsx")
}

// Defaults returns a Config pre-filled with sensible starting values.
func Defaults() Config {
	return Config{
		MiniMaxBaseURL:  "https://api.minimax.io/v1",
		MiniMaxModel:    "MiniMax-M3",
		FilePath:        DefaultWorklogPath(),
		Categories:      "VAPOMAN",
		Types:           "New",
		IntervalMinutes: 15,
		Monitor:         -1,
		PopupPosition:   "bottom-right",
		Language:        "Czech",
		Prompt:          "You are helping fill in a work timesheet. Look at the screenshot and write ONE concise sentence, in first person past tense, describing the concrete work task the user was doing (e.g. which app, document, ticket, or topic). Do not describe the screenshot itself; describe the work. No preamble.",
	}
}

var (
	mu     sync.RWMutex
	cached *Config
)

// Dir returns the per-user config directory, creating it if necessary.
func Dir() (string, error) {
	base, err := os.UserConfigDir() // %APPDATA% on Windows
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Quarterlog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config from disk, falling back to defaults for a missing or partial file.
func Load() (Config, error) {
	mu.Lock()
	defer mu.Unlock()

	cfg := Defaults()
	p, err := path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			c := cfg
			cached = &c
			return cfg, nil // first run: defaults, not an error
		}
		return cfg, err
	}
	// Unmarshal over defaults so new fields keep their default values.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	c := cfg
	cached = &c
	return cfg, nil
}

// Save writes the config to disk atomically and updates the cache.
func Save(cfg Config) error {
	mu.Lock()
	defer mu.Unlock()

	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	c := cfg
	cached = &c
	return nil
}

// Current returns the last loaded/saved config, loading from disk on first use.
func Current() Config {
	mu.RLock()
	if cached != nil {
		c := *cached
		mu.RUnlock()
		return c
	}
	mu.RUnlock()
	c, _ := Load()
	return c
}
