package orchestrator

import "gorm.io/gorm"

func finishTx(tx *gorm.DB, errp *error) {
	if *errp == nil {
		if commitErr := tx.Commit().Error; commitErr != nil {
			*errp = commitErr
		}
		return
	}
	tx.Rollback()
}
