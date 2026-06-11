package appindex

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Entry represents a single app's local path mapping.
type Entry struct {
	Path     string `yaml:"path"`
	LastSeen string `yaml:"last_seen"`
}

// filePath returns the absolute path to the app index file.
func filePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".fyra", "apps.yaml"), nil
}

// Load reads the app index from disk. Returns an empty map if the file doesn't exist.
func Load() (map[string]Entry, error) {
	p, err := filePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]Entry), nil
		}
		return nil, fmt.Errorf("read app index: %w", err)
	}

	var entries map[string]Entry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse app index: %w", err)
	}
	if entries == nil {
		entries = make(map[string]Entry)
	}
	return entries, nil
}

// Save writes the app index to disk with 0600 permissions.
func Save(entries map[string]Entry) error {
	p, err := filePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("create .fyra dir: %w", err)
	}

	data, err := yaml.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal app index: %w", err)
	}

	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("write app index: %w", err)
	}
	return nil
}

// Register adds or updates an entry in the app index with the current timestamp.
func Register(slug, absPath string) error {
	entries, err := Load()
	if err != nil {
		return err
	}
	entries[slug] = Entry{
		Path:     absPath,
		LastSeen: time.Now().UTC().Format(time.RFC3339),
	}
	return Save(entries)
}

// Lookup returns the local path for a slug. Returns ("", false) if not found.
func Lookup(slug string) (string, bool) {
	entries, err := Load()
	if err != nil {
		return "", false
	}
	e, ok := entries[slug]
	return e.Path, ok
}
