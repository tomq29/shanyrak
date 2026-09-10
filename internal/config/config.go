// Package config reads the shanyrak.toml shared with the Python scraper. The
// server only needs to know where the databases are and how they are named.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Path     string
	DataDir  string
	Searches []string
	Telegram Telegram
}

// Telegram tunes the digest; the bot token and chat id come from the
// environment, so secrets never sit in the config file.
type Telegram struct {
	Deviation float64
	Interval  time.Duration
}

type file struct {
	DataDir  string                    `toml:"data_dir"`
	Searches map[string]map[string]any `toml:"searches"`
	Telegram struct {
		Deviation *float64 `toml:"deviation"`
		Interval  string   `toml:"interval"`
	} `toml:"telegram"`
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

	telegram := Telegram{Deviation: -10, Interval: time.Hour}
	if parsed.Telegram.Deviation != nil {
		telegram.Deviation = *parsed.Telegram.Deviation
	}
	if parsed.Telegram.Interval != "" {
		telegram.Interval, err = time.ParseDuration(parsed.Telegram.Interval)
		if err != nil {
			return Config{}, fmt.Errorf("%s: telegram.interval: %w", path, err)
		}
	}

	return Config{
		Path:     path,
		DataDir:  dataDir,
		Searches: names,
		Telegram: telegram,
	}, nil
}

func (c Config) DatabasePath(search string) string {
	return filepath.Join(c.DataDir, search+".db")
}
