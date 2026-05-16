package db

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const logSettingID uint = 1

type LogSetting struct {
	ID         uint  `gorm:"primaryKey;autoIncrement:false"`
	MaxEntries int   `gorm:"not null"`
	MaxBytes   int64 `gorm:"not null"`
}

func GetLogSetting(ctx context.Context) (*LogSetting, error) {
	var setting LogSetting
	err := DB(ctx).First(&setting, logSettingID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

func SaveLogSetting(ctx context.Context, setting LogSetting) error {
	setting.ID = logSettingID
	return DB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"max_entries",
			"max_bytes",
		}),
	}).Create(&setting).Error
}
