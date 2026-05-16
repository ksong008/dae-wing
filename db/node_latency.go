package db

import (
	"context"
	"time"

	"gorm.io/gorm/clause"
)

type NodeLatencyResult struct {
	NodeID    uint `gorm:"primaryKey;autoIncrement:false"`
	LatencyMs *int32
	Alive     bool      `gorm:"not null"`
	TestedAt  time.Time `gorm:"not null;index"`
	Message   *string
	UpdatedAt time.Time `gorm:"not null"`

	Node Node `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func UpsertNodeLatencyResults(ctx context.Context, results []NodeLatencyResult) error {
	if len(results) == 0 {
		return nil
	}

	now := time.Now()
	for i := range results {
		results[i].UpdatedAt = now
	}

	return DB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"latency_ms",
			"alive",
			"tested_at",
			"message",
			"updated_at",
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "excluded.tested_at >= node_latency_results.tested_at"},
		}},
	}).Create(&results).Error
}

func ListNodeLatencyResults(ctx context.Context, ids []uint, cutoff time.Time) ([]NodeLatencyResult, error) {
	q := DB(ctx).Model(&NodeLatencyResult{}).Where("tested_at >= ?", cutoff)
	if len(ids) > 0 {
		q = q.Where("node_id in ?", ids)
	}

	var results []NodeLatencyResult
	if err := q.Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func DeleteNodeLatencyResults(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return DB(ctx).Where("node_id in ?", ids).Delete(&NodeLatencyResult{}).Error
}
