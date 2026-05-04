/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae/pkg/config_parser"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func CreateGroup(ctx context.Context, name string, policy string, policyParams []config_parser.Param) (*db.Group, error) {
	if err := common.ValidateId(name); err != nil {
		return nil, err
	}
	params := make([]db.GroupPolicyParam, len(policyParams))
	for i := range params {
		params[i].Unmarshal(&policyParams[i])
	}
	model := db.Group{
		Name:         name,
		Policy:       policy,
		PolicyParams: params,
	}
	if err := db.DB(ctx).Create(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func GetGroup(ctx context.Context, id uint) (*db.Group, error) {
	var group db.Group
	if err := preloadGroupReadQuery(db.DB(ctx)).First(&group, id).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

func ListGroups(ctx context.Context, id *uint, name *string) ([]db.Group, error) {
	q := preloadGroupReadQuery(db.DB(ctx)).Model(&db.Group{})
	if id != nil {
		q = q.Where("id = ?", *id)
	}
	if name != nil {
		q = q.Where("name = ?", *name)
	}
	var models []db.Group
	if err := q.Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func preloadGroupReadQuery(q *gorm.DB) *gorm.DB {
	return q.Preload("Node").
		Preload("PolicyParams").
		Preload("SubscriptionBindings").
		Preload("SubscriptionBindings.Subscription").
		Preload("SubscriptionBindings.Subscription.Node")
}

func GetGroupNodes(ctx context.Context, id uint) ([]db.Node, error) {
	group := db.Group{ID: id}
	var nodes []db.Node
	if err := db.DB(ctx).Model(&group).Association("Node").Find(&nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func GetGroupSubscriptions(ctx context.Context, id uint) ([]db.GroupSubscription, error) {
	var bindings []db.GroupSubscription
	if err := db.DB(ctx).
		Where("group_id = ?", id).
		Preload("Subscription").
		Preload("Subscription.Node").
		Find(&bindings).Error; err != nil {
		return nil, err
	}
	return bindings, nil
}

func GetGroupPolicyParams(ctx context.Context, id uint) ([]db.GroupPolicyParam, error) {
	group := db.Group{ID: id}
	var params []db.GroupPolicyParam
	if err := db.DB(ctx).Model(&group).Association("PolicyParams").Find(&params); err != nil {
		return nil, err
	}
	return params, nil
}

func RenameGroup(ctx context.Context, id uint, name string) (int32, error) {
	if err := common.ValidateId(name); err != nil {
		return 0, err
	}
	tx := db.BeginTx(ctx)
	defer func() {
		if tx.Error != nil {
			tx.Rollback()
		}
	}()

	group := db.Group{ID: id}
	if err := tx.Model(&group).First(&group).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	q := tx.Model(&group).Update("name", name)
	if q.Error != nil {
		tx.Rollback()
		return 0, q.Error
	}
	if q.Statement.Changed() {
		if err := bumpGroupVersion(tx, group.ID); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return int32(q.RowsAffected), nil
}

func DeleteGroup(ctx context.Context, id uint) (int32, error) {
	tx := db.BeginTx(ctx)
	defer func() {
		if tx.Error != nil {
			tx.Rollback()
		}
	}()

	group := db.Group{ID: id}
	q := tx.Select(clause.Associations).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "name"}}}).
		Delete(&group)
	if q.Error != nil {
		tx.Rollback()
		return 0, q.Error
	}
	if err := tx.Model(&db.Group{ID: id}).Association("Node").Clear(); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Where("group_id = ?", id).Delete(&db.GroupSubscription{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Where("group_id = ?", id).Delete(&db.GroupPolicyParam{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := bumpGroupVersion(tx, group.ID); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return int32(q.RowsAffected), nil
}

func SetGroupPolicy(ctx context.Context, id uint, policy string, policyParams []config_parser.Param) (int32, error) {
	tx := db.BeginTx(ctx)
	defer func() {
		if tx.Error != nil {
			tx.Rollback()
		}
	}()

	q := tx.Model(&db.Group{ID: id}).Update("policy", policy)
	if q.Error != nil {
		tx.Rollback()
		return 0, q.Error
	}
	if q.RowsAffected == 0 {
		tx.Rollback()
		return 0, nil
	}

	params := make([]db.GroupPolicyParam, len(policyParams))
	for i := range params {
		params[i].Unmarshal(&policyParams[i])
	}
	if err := tx.Model(&db.Group{ID: id}).Association("PolicyParams").Replace(params); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := bumpGroupVersion(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return int32(q.RowsAffected), nil
}

func AddGroupSubscriptions(ctx context.Context, id uint, subscriptionIDs []uint, nameFilterRegex *string) (int32, error) {
	normalizedRegex, err := normalizeGroupNameFilterRegex(nameFilterRegex)
	if err != nil {
		return 0, err
	}
	tx := db.BeginTx(ctx)
	if tx.Error != nil {
		return 0, tx.Error
	}
	defer func() {
		if tx.Error != nil {
			tx.Rollback()
		}
	}()
	if err := ensureGroupExists(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := ensureIDsExist(tx, &db.Subscription{}, subscriptionIDs, "subscription"); err != nil {
		tx.Rollback()
		return 0, err
	}

	bindings := make([]db.GroupSubscription, 0, len(subscriptionIDs))
	for _, subscriptionID := range subscriptionIDs {
		bindings = append(bindings, db.GroupSubscription{
			GroupID:         id,
			SubscriptionID:  subscriptionID,
			NameFilterRegex: normalizedRegex,
		})
	}
	if len(bindings) > 0 {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "group_id"}, {Name: "subscription_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"name_filter_regex": normalizedRegex,
			}),
		}).Create(&bindings).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := bumpGroupVersion(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return int32(len(subscriptionIDs)), nil
}

func DeleteGroupSubscriptions(ctx context.Context, id uint, subscriptionIDs []uint) (int32, error) {
	tx := db.BeginTx(ctx)
	if tx.Error != nil {
		return 0, tx.Error
	}
	defer func() {
		if tx.Error != nil {
			tx.Rollback()
		}
	}()
	if err := ensureGroupExists(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := ensureIDsExist(tx, &db.Subscription{}, subscriptionIDs, "subscription"); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Where("group_id = ? AND subscription_id in ?", id, subscriptionIDs).Delete(&db.GroupSubscription{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := bumpGroupVersion(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return int32(len(subscriptionIDs)), nil
}

func AddGroupNodes(ctx context.Context, id uint, nodeIDs []uint) (int32, error) {
	tx := db.BeginTx(ctx)
	if tx.Error != nil {
		return 0, tx.Error
	}
	defer func() {
		if tx.Error != nil {
			tx.Rollback()
		}
	}()
	if err := ensureGroupExists(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := ensureIDsExist(tx, &db.Node{}, nodeIDs, "node"); err != nil {
		tx.Rollback()
		return 0, err
	}

	nodes := make([]db.Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodes = append(nodes, db.Node{ID: nodeID})
	}
	if err := tx.Model(&db.Group{ID: id}).Association("Node").Append(nodes); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := bumpGroupVersion(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return int32(len(nodeIDs)), nil
}

func DeleteGroupNodes(ctx context.Context, id uint, nodeIDs []uint) (int32, error) {
	tx := db.BeginTx(ctx)
	if tx.Error != nil {
		return 0, tx.Error
	}
	defer func() {
		if tx.Error != nil {
			tx.Rollback()
		}
	}()
	if err := ensureGroupExists(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := ensureIDsExist(tx, &db.Node{}, nodeIDs, "node"); err != nil {
		tx.Rollback()
		return 0, err
	}

	nodes := make([]db.Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodes = append(nodes, db.Node{ID: nodeID})
	}
	if err := tx.Model(&db.Group{ID: id}).Association("Node").Delete(nodes); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := bumpGroupVersion(tx, id); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return int32(len(nodeIDs)), nil
}

func MatchedNodesForGroupBinding(binding *db.GroupSubscription) ([]db.Node, error) {
	nodes := binding.Subscription.Node
	if binding.NameFilterRegex == nil || *binding.NameFilterRegex == "" {
		return nodes, nil
	}

	re, err := regexp.Compile(*binding.NameFilterRegex)
	if err != nil {
		return nil, err
	}
	var matched []db.Node
	for _, node := range nodes {
		if re.MatchString(node.Name) {
			matched = append(matched, node)
		}
	}
	return matched, nil
}

func normalizeGroupNameFilterRegex(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	if _, err := regexp.Compile(trimmed); err != nil {
		return nil, fmt.Errorf("invalid name filter regex: %w", err)
	}
	return &trimmed, nil
}

func bumpGroupVersion(d *gorm.DB, id uint) error {
	return d.Model(db.Group{ID: id}).Update("version", gorm.Expr("version + 1")).Error
}

func ensureGroupExists(d *gorm.DB, id uint) error {
	var count int64
	if err := d.Model(&db.Group{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("group %d: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

func ensureIDsExist(d *gorm.DB, model any, ids []uint, label string) error {
	if len(ids) == 0 {
		return nil
	}
	uniqueIDs := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	var foundIDs []uint
	if err := d.Model(model).Where("id in ?", uniqueIDs).Pluck("id", &foundIDs).Error; err != nil {
		return err
	}
	found := make(map[uint]struct{}, len(foundIDs))
	for _, id := range foundIDs {
		found[id] = struct{}{}
	}
	missing := make([]string, 0)
	for _, id := range uniqueIDs {
		if _, ok := found[id]; !ok {
			missing = append(missing, fmt.Sprintf("%d", id))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s ids not found: %s: %w", label, strings.Join(missing, ", "), gorm.ErrRecordNotFound)
	}
	return nil
}
