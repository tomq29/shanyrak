// Package config reads the shanyrak.toml shared with the Python scraper. The
// server only needs to know where the databases are and how they are named.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Path     string
	DataDir  string
	Searches []string
}

type file struct {
	DataDir  string                    `toml:"data_dir"`
	Searches map[string]map[string]any `toml:"searches"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	var parsed file
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(parsed.Searches) == 0 {
		return Config{}, fmt.Errorf("%s: no [searches.*] sections", path)
	}

	names := make([]string, 0, len(parsed.Searches))
	for name := range parsed.Searches {
		names = append(names, name)
	}
	sort.Strings(names)

	dataDir := parsed.DataDir
	if dataDir == "" {
		dataDir = "data"
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(filepath.Dir(path), dataDir)
	}

	return Config{Path: path, DataDir: dataDir, Searches: names}, nil
}

func (c Config) DatabasePath(search string) string {
	return filepath.Join(c.DataDir, search+".db")
}
