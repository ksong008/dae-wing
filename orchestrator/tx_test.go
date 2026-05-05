package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/daeuniverse/dae-wing/db"
	"gorm.io/gorm"
)

func TestFinishTxPropagatesCommitError(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}

	var err error
	finishTx(db.DB(context.Background()), &err)
	if !errors.Is(err, gorm.ErrInvalidTransaction) {
		t.Fatalf("finishTx() error = %v, want invalid transaction", err)
	}
}

func TestFinishTxPreservesOriginalError(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}

	original := errors.New("write failed")
	err := original
	finishTx(db.DB(context.Background()), &err)
	if !errors.Is(err, original) {
		t.Fatalf("finishTx() error = %v, want original error", err)
	}
}
