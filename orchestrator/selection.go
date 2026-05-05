/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"fmt"

	"github.com/daeuniverse/dae-wing/db"
)

func SelectConfig(ctx context.Context, id uint) (n int32, err error) {
	tx := db.BeginTx(ctx)
	defer finishTx(tx, &err)
	q := tx.Model(&db.Config{}).Where("selected = ?", true).Update("selected", false)
	if err = q.Error; err != nil {
		return 0, err
	}
	q = tx.Model(&db.Config{ID: id}).Update("selected", true)
	if err = q.Error; err != nil {
		return 0, err
	}
	if q.RowsAffected == 0 {
		return 0, fmt.Errorf("no such config")
	}
	return 1, nil
}

func SelectDNS(ctx context.Context, id uint) (n int32, err error) {
	tx := db.BeginTx(ctx)
	defer finishTx(tx, &err)
	q := tx.Model(&db.Dns{}).Where("selected = ?", true).Update("selected", false)
	if err = q.Error; err != nil {
		return 0, err
	}
	q = tx.Model(&db.Dns{ID: id}).Update("selected", true)
	if err = q.Error; err != nil {
		return 0, err
	}
	if q.RowsAffected == 0 {
		return 0, fmt.Errorf("no such dns")
	}
	return 1, nil
}

func SelectRouting(ctx context.Context, id uint) (n int32, err error) {
	tx := db.BeginTx(ctx)
	defer finishTx(tx, &err)
	q := tx.Model(&db.Routing{}).Where("selected = ?", true).Update("selected", false)
	if err = q.Error; err != nil {
		return 0, err
	}
	q = tx.Model(&db.Routing{ID: id}).Update("selected", true)
	if err = q.Error; err != nil {
		return 0, err
	}
	if q.RowsAffected == 0 {
		return 0, fmt.Errorf("no such routing")
	}
	return 1, nil
}
