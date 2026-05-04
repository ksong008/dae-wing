package db

import (
	"context"
	"testing"
)

func TestInitDatabaseEnablesSQLiteForeignKeys(t *testing.T) {
	if err := InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("InitDatabase() error = %v", err)
	}

	var enabled int
	if err := DB(context.Background()).Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", enabled)
	}
}
