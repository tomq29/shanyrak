package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shanyrak.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadListsSearchesInOrder(t *testing.T) {
	path := write(t, `
data_dir = "db"
[searches.astana]
section = "/prodazha/kvartiry/astana/"
[searches.almaty]
section = "/prodazha/kvartiry/almaty/"
`)

	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(conf.Searches) != 2 || conf.Searches[0] != "almaty" {
		t.Fatalf("searches = %v, want them sorted", conf.Searches)
	}
	if conf.DataDir != filepath.Join(filepath.Dir(path), "db") {
		t.Fatalf("data dir = %s, want it relative to the config", conf.DataDir)
	}
	if got := conf.DatabasePath("astana"); !strings.HasSuffix(got, "db/astana.db") {
		t.Fatalf("database path = %s", got)
	}
}

func TestDataDirDefaultsToData(t *testing.T) {
	conf, err := Load(write(t, "[searches.astana]\nsection = \"/x/\"\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if filepath.Base(conf.DataDir) != "data" {
		t.Fatalf("data dir = %s, want data", conf.DataDir)
	}
}

func TestAbsoluteDataDirIsKept(t *testing.T) {
	conf, err := Load(write(t, "data_dir = \"/srv/shanyrak\"\n[searches.astana]\nsection = \"/x/\"\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if conf.DataDir != "/srv/shanyrak" {
		t.Fatalf("data dir = %s", conf.DataDir)
	}
}

func TestLoadRejectsConfigWithoutSearches(t *testing.T) {
	if _, err := Load(write(t, "data_dir = \"db\"\n")); err == nil {
		t.Fatal("want an error for a config without searches")
	}
}

func TestLoadReportsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Fatal("want an error for a missing file")
	}
}

func TestLoadReportsBrokenToml(t *testing.T) {
	if _, err := Load(write(t, "data_dir = \n")); err == nil {
		t.Fatal("want a parse error")
	}
}
