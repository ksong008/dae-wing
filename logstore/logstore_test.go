package logstore

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/sirupsen/logrus"
)

func initTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	cfgDir := t.TempDir()
	if err := db.InitDatabase(cfgDir); err != nil {
		t.Fatal(err)
	}
	store := New()
	if err := store.Init(cfgDir); err != nil {
		t.Fatal(err)
	}
	return store, cfgDir
}

func TestStoreInitUsesConfigLogDirectory(t *testing.T) {
	store, cfgDir := initTestStore(t)

	wantDir := filepath.Join(cfgDir, "logs")
	if store.dir != wantDir {
		t.Fatalf("log dir = %q, want %q", store.dir, wantDir)
	}
	if store.logPath != filepath.Join(wantDir, "current.jsonl") {
		t.Fatalf("log path = %q", store.logPath)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("settings.json should not be created, stat error = %v", err)
	}
}

func TestStoreSettingsAllowSmallerThanDefaults(t *testing.T) {
	store, _ := initTestStore(t)

	settings, err := store.SetSettings(Settings{
		MaxEntries: MinMaxEntries + 100,
		MaxBytes:   MinMaxBytes + 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.MaxEntries != MinMaxEntries+100 {
		t.Fatalf("MaxEntries = %d, want %d", settings.MaxEntries, MinMaxEntries+100)
	}
	if settings.MaxBytes != MinMaxBytes+1024 {
		t.Fatalf("MaxBytes = %d, want %d", settings.MaxBytes, MinMaxBytes+1024)
	}

	settings, err = store.SetSettings(Settings{MaxEntries: 1, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if settings.MaxEntries != MinMaxEntries {
		t.Fatalf("MaxEntries = %d, want lower bound %d", settings.MaxEntries, MinMaxEntries)
	}
	if settings.MaxBytes != MinMaxBytes {
		t.Fatalf("MaxBytes = %d, want lower bound %d", settings.MaxBytes, MinMaxBytes)
	}
}

func TestStoreSettingsPersistInDatabase(t *testing.T) {
	store, cfgDir := initTestStore(t)
	expected := Settings{
		MaxEntries: MinMaxEntries + 10,
		MaxBytes:   MinMaxBytes + 2048,
	}
	if _, err := store.SetSettings(expected); err != nil {
		t.Fatal(err)
	}

	reloaded := New()
	if err := reloaded.Init(cfgDir); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings(); got != expected {
		t.Fatalf("reloaded settings = %#v, want %#v", got, expected)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "logs", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("settings.json should not be created, stat error = %v", err)
	}
}

func TestStoreQueryAndClear(t *testing.T) {
	store, _ := initTestStore(t)

	now := time.Now()
	entries := []*logrus.Entry{
		{Time: now, Level: logrus.InfoLevel, Message: "runtime ready"},
		{Time: now.Add(time.Second), Level: logrus.WarnLevel, Message: "subscription failed", Data: logrus.Fields{"subscription": "daily"}},
	}
	for _, entry := range entries {
		if err := store.Fire(entry); err != nil {
			t.Fatal(err)
		}
	}

	warnEntries, err := store.Query(QueryFilter{Level: "warn"})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnEntries) != 1 || warnEntries[0].Message != "subscription failed" {
		t.Fatalf("warn query = %#v", warnEntries)
	}

	fieldEntries, err := store.Query(QueryFilter{Query: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fieldEntries) != 1 || fieldEntries[0].Fields["subscription"] != "daily" {
		t.Fatalf("field query = %#v", fieldEntries)
	}

	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	empty, err := store.Query(QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("query after clear returned %d entries", len(empty))
	}
}

func TestStorePruneKeepsLatestEntries(t *testing.T) {
	store, _ := initTestStore(t)
	if _, err := store.SetSettings(Settings{MaxEntries: MinMaxEntries, MaxBytes: MinMaxBytes}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for i := 0; i < MinMaxEntries+20; i++ {
		if err := store.Fire(&logrus.Entry{
			Time:    now.Add(time.Duration(i) * time.Second),
			Level:   logrus.InfoLevel,
			Message: fmt.Sprintf("entry-%03d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Prune(); err != nil {
		t.Fatal(err)
	}

	entries, err := store.Query(QueryFilter{Limit: MaxMaxEntries})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MinMaxEntries {
		t.Fatalf("entries = %d, want %d", len(entries), MinMaxEntries)
	}
	if entries[0].Message != "entry-020" {
		t.Fatalf("first kept entry = %q, want entry-020", entries[0].Message)
	}
	if entries[len(entries)-1].Message != fmt.Sprintf("entry-%03d", MinMaxEntries+19) {
		t.Fatalf("last kept entry = %q", entries[len(entries)-1].Message)
	}
}
