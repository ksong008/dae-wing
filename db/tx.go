/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package db

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

func FinishTx(tx *gorm.DB, currentErr error) error {
	if currentErr == nil {
		if commitErr := tx.Commit().Error; commitErr != nil {
			return commitErr
		}
		return nil
	}
	if rollbackErr := tx.Rollback().Error; rollbackErr != nil && !errors.Is(rollbackErr, gorm.ErrInvalidTransaction) {
		return fmt.Errorf("%w; rollback: %v", currentErr, rollbackErr)
	}
	return currentErr
}
