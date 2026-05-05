/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"errors"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNodeDuplicated = errors.New("node already exists")

type ImportArgument struct {
	Link string
	Tag  *string
}

type NodeImportResult struct {
	Link  string
	Error *string
	Node  *db.Node
}

func ImportNodes(d *gorm.DB, abortError bool, subscriptionID *uint, args []ImportArgument) (results []*NodeImportResult, err error) {
	for _, arg := range args {
		model, importErr := importNode(d, subscriptionID, &arg)
		if importErr != nil {
			if abortError && !errors.Is(importErr, ErrNodeDuplicated) {
				return nil, importErr
			}
			message := importErr.Error()
			results = append(results, &NodeImportResult{
				Link:  arg.Link,
				Error: &message,
			})
			continue
		}
		results = append(results, &NodeImportResult{
			Link: arg.Link,
			Node: model,
		})
	}
	return results, nil
}

func UpdateNode(ctx context.Context, id uint, link *string, tag *string) (node *db.Node, err error) {
	tx := db.BeginTx(ctx)
	defer finishTx(tx, &err)

	var current db.Node
	if err = tx.First(&current, id).Error; err != nil {
		return nil, err
	}

	finalLink := current.Link
	if link != nil {
		finalLink = *link
	}
	finalTag := current.Tag
	if tag != nil {
		if *tag == "" {
			finalTag = nil
		} else {
			finalTag = tag
		}
	}

	model, err := db.NewNodeModel(finalLink, finalTag, current.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if err = tx.Model(&db.Node{ID: id}).Updates(model).Error; err != nil {
		return nil, err
	}
	if err = autoUpdateGroupVersionsByNodeIDs(tx, []uint{id}); err != nil {
		return nil, err
	}
	if link != nil {
		removeNodeLatencyResults([]uint{id})
	}
	return getNode(tx, id)
}

func DeleteNode(ctx context.Context, id uint) (deleted bool, err error) {
	count, err := DeleteNodes(ctx, []uint{id})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func DeleteNodes(ctx context.Context, ids []uint) (count int32, err error) {
	if len(ids) == 0 {
		return 0, nil
	}

	tx := db.BeginTx(ctx)
	defer finishTx(tx, &err)

	if err = autoUpdateGroupVersionsByNodeIDs(tx, ids); err != nil {
		return 0, err
	}

	q := tx.Where("id in ?", ids).
		Select(clause.Associations).
		Delete(&db.Node{})
	if q.Error != nil {
		return 0, q.Error
	}
	removeNodeLatencyResults(ids)
	return int32(q.RowsAffected), nil
}

func GetNode(ctx context.Context, id uint) (*db.Node, error) {
	return getNode(db.DB(ctx), id)
}

func ListNodes(ctx context.Context, id *uint, subscriptionID *uint, independent *bool, afterID *uint, limit *int) ([]db.Node, int64, error) {
	baseQuery := db.DB(ctx).Model(&db.Node{})
	if id != nil {
		baseQuery = baseQuery.Where("id = ?", *id)
	}
	if subscriptionID != nil {
		baseQuery = baseQuery.Where("subscription_id = ?", *subscriptionID)
	} else if independent != nil {
		if *independent {
			baseQuery = baseQuery.Where("subscription_id is null")
		} else {
			baseQuery = baseQuery.Where("subscription_id is not null")
		}
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query := baseQuery.Order("id asc")
	if afterID != nil {
		query = query.Where("id > ?", *afterID)
	}
	if limit != nil && *limit > 0 {
		query = query.Limit(*limit)
	}

	var nodes []db.Node
	if err := query.Find(&nodes).Error; err != nil {
		return nil, 0, err
	}
	return nodes, total, nil
}

func importNode(d *gorm.DB, subscriptionID *uint, arg *ImportArgument) (*db.Node, error) {
	if arg.Tag != nil {
		if err := common.ValidateTag(*arg.Tag); err != nil {
			return nil, err
		}
	}

	model, err := db.NewNodeModel(arg.Link, arg.Tag, subscriptionID)
	if err != nil {
		return nil, err
	}

	var count int64
	if err = d.Model(&db.Node{}).
		Where("link = ?", arg.Link).
		Where("subscription_id = ?", subscriptionID).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrNodeDuplicated
	}
	if err = d.Create(model).Error; err != nil {
		return nil, err
	}
	return model, nil
}

func autoUpdateGroupVersionsByNodeIDs(d *gorm.DB, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}

	var sys db.System
	if err := d.Model(&db.System{}).FirstOrCreate(&sys).Error; err != nil {
		return err
	}
	if !sys.Running {
		return nil
	}

	return d.Exec(`update groups
                set version = groups.version + 1
                from groups g
                    inner join group_nodes
                    on g.system_id = ? and g.id = group_nodes.group_id and group_nodes.node_id in ?
				where g.id = groups.id`, sys.ID, ids).Error
}

func getNode(d *gorm.DB, id uint) (*db.Node, error) {
	var node db.Node
	if err := d.First(&node, id).Error; err != nil {
		return nil, err
	}
	return &node, nil
}
