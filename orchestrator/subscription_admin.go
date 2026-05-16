/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/go-co-op/gocron"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionImportResult struct {
	Link             string
	NodeImportResult []*NodeImportResult
	Subscription     *db.Subscription
}

type SubscriptionUpdateInput struct {
	Link       *string
	Tag        *string
	CronExp    *string
	CronEnable *bool
}

var (
	subscriptionScheduler   *gocron.Scheduler
	subscriptionSchedulerMu sync.Mutex
)

const subscriptionRefreshReloadTimeout = 30 * time.Second

func ImportSubscription(ctx context.Context, rollbackError bool, arg ImportArgument) (result *SubscriptionImportResult, err error) {
	if arg.Tag != nil {
		if err = common.ValidateTag(*arg.Tag); err != nil {
			return nil, err
		}
	}

	links, err := FetchSubscriptionLinks(ctx, arg.Link)
	if err != nil {
		return nil, err
	}

	tx := db.BeginTx(ctx)
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	model := db.Subscription{
		UpdatedAt:  time.Now(),
		Tag:        arg.Tag,
		Link:       arg.Link,
		Status:     "",
		Info:       "",
		CronExp:    "10 */6 * * *",
		CronEnable: true,
	}
	if err = tx.Create(&model).Error; err != nil {
		return nil, err
	}

	nodeArgs := make([]ImportArgument, 0, len(links))
	for _, link := range links {
		nodeArgs = append(nodeArgs, ImportArgument{Link: link})
	}
	nodeResults, err := ImportNodes(tx, rollbackError, &model.ID, nodeArgs)
	if err != nil {
		return nil, err
	}
	if !hasAnyImportedNode(nodeResults) {
		return nil, fmt.Errorf("no any valid node can be imported")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, err
	}
	AddSubscriptionUpdateScheduler(ctx, model.ID)
	return &SubscriptionImportResult{
		Link:             arg.Link,
		NodeImportResult: nodeResults,
		Subscription:     &model,
	}, nil
}

func RefreshSubscription(ctx context.Context, id uint) (sub *db.Subscription, err error) {
	var current db.Subscription
	if err = db.DB(ctx).Where(&db.Subscription{ID: id}).First(&current).Error; err != nil {
		return nil, err
	}

	links, err := FetchSubscriptionLinks(ctx, current.Link)
	if err != nil {
		return nil, err
	}

	tx := db.BeginTx(ctx)
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	subQuery := tx.Raw(`select nodes.id as id
                from nodes
                inner join group_nodes on group_nodes.node_id = nodes.id
                where subscription_id = ?`, id)

	var preservedNodes []db.Node
	if err = tx.Where("subscription_id = ?", id).
		Where("id in (?)", subQuery).
		Find(&preservedNodes).Error; err != nil {
		return nil, err
	}

	var removedNodes []db.Node
	if err = tx.Where("subscription_id = ?", id).
		Where("id not in (?)", subQuery).
		Find(&removedNodes).Error; err != nil {
		return nil, err
	}

	if err = tx.Where("subscription_id = ?", id).
		Where("id not in (?)", subQuery).
		Select(clause.Associations).
		Delete(&db.Node{}).Error; err != nil {
		return nil, err
	}

	nodeArgs := make([]ImportArgument, 0, len(links))
	for _, link := range links {
		nodeArgs = append(nodeArgs, ImportArgument{Link: link})
	}
	nodeResults, updatedPreservedNodeIDs, err := refreshSubscriptionNodes(tx, id, nodeArgs, preservedNodes)
	if err != nil {
		return nil, err
	}
	if !hasAnyImportedNode(nodeResults) {
		return nil, fmt.Errorf("interrupt to update subscription: no any valid node can be imported")
	}

	if err = autoUpdateGroupVersionsByNodeIDs(tx, updatedPreservedNodeIDs); err != nil {
		return nil, err
	}
	if err = tx.Model(&current).
		Clauses(clause.Returning{}).
		Where(&db.Subscription{ID: id}).
		Update("updated_at", time.Now()).Error; err != nil {
		return nil, err
	}
	if err = autoUpdateGroupVersionsBySubscriptionIDs(tx, []uint{id}); err != nil {
		return nil, err
	}
	removedIDs := make([]uint, 0, len(removedNodes))
	for _, node := range removedNodes {
		removedIDs = append(removedIDs, node.ID)
	}
	latencyInvalidatedIDs := append(removedIDs, updatedPreservedNodeIDs...)
	if err = deleteNodeLatencyResultsWithTx(tx, latencyInvalidatedIDs); err != nil {
		return nil, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, err
	}
	removeNodeLatencyResults(latencyInvalidatedIDs)
	if err = reloadRuntimeAfterSubscriptionRefresh(ctx); err != nil {
		return nil, err
	}
	return &current, nil
}

func refreshSubscriptionNodes(
	tx *gorm.DB,
	subscriptionID uint,
	args []ImportArgument,
	preservedNodes []db.Node,
) (results []*NodeImportResult, updatedPreservedNodeIDs []uint, err error) {
	subscriptionIDPtr := &subscriptionID
	type nodeCandidate struct {
		arg   ImportArgument
		model *db.Node
	}

	candidates := make([]nodeCandidate, 0, len(args))
	incomingNameCounts := make(map[string]int, len(args))
	for _, arg := range args {
		model, importErr := db.NewNodeModel(arg.Link, arg.Tag, subscriptionIDPtr)
		if importErr != nil {
			message := importErr.Error()
			results = append(results, &NodeImportResult{
				Link:  arg.Link,
				Error: &message,
			})
			continue
		}
		candidates = append(candidates, nodeCandidate{
			arg:   arg,
			model: model,
		})
		incomingNameCounts[model.Name]++
	}

	preservedNameCounts := make(map[string]int, len(preservedNodes))
	preservedByName := make(map[string]*db.Node, len(preservedNodes))
	for i := range preservedNodes {
		node := &preservedNodes[i]
		preservedNameCounts[node.Name]++
		preservedByName[node.Name] = node
	}

	for _, candidate := range candidates {
		model := candidate.model
		if incomingNameCounts[model.Name] == 1 && preservedNameCounts[model.Name] == 1 {
			preserved := preservedByName[model.Name]
			updatedModel := *model
			updatedModel.ID = preserved.ID

			if subscriptionNodeChanged(preserved, model) {
				if err = tx.Model(&db.Node{ID: preserved.ID}).Updates(map[string]any{
					"link":            model.Link,
					"name":            model.Name,
					"address":         model.Address,
					"protocol":        model.Protocol,
					"tag":             model.Tag,
					"subscription_id": model.SubscriptionID,
				}).Error; err != nil {
					return nil, nil, err
				}
				updatedPreservedNodeIDs = append(updatedPreservedNodeIDs, preserved.ID)
			}

			results = append(results, &NodeImportResult{
				Link: candidate.arg.Link,
				Node: &updatedModel,
			})
			continue
		}

		var count int64
		if err = tx.Model(&db.Node{}).
			Where("link = ?", candidate.arg.Link).
			Where("subscription_id = ?", subscriptionIDPtr).
			Count(&count).Error; err != nil {
			return nil, nil, err
		}
		if count > 0 {
			message := ErrNodeDuplicated.Error()
			results = append(results, &NodeImportResult{
				Link:  candidate.arg.Link,
				Error: &message,
			})
			continue
		}
		if err = tx.Create(model).Error; err != nil {
			return nil, nil, err
		}
		results = append(results, &NodeImportResult{
			Link: candidate.arg.Link,
			Node: model,
		})
	}
	return results, updatedPreservedNodeIDs, nil
}

func subscriptionNodeChanged(current *db.Node, next *db.Node) bool {
	return current.Link != next.Link ||
		current.Name != next.Name ||
		current.Address != next.Address ||
		current.Protocol != next.Protocol ||
		current.SubscriptionID == nil ||
		next.SubscriptionID == nil ||
		*current.SubscriptionID != *next.SubscriptionID
}

func reloadRuntimeAfterSubscriptionRefresh(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	modified, err := runtimeModified(ctx)
	if err != nil {
		return err
	}
	if !modified {
		return nil
	}

	reloadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), subscriptionRefreshReloadTimeout)
	defer cancel()
	if err := RestoreRunningState(reloadCtx); err != nil {
		return fmt.Errorf("failed to reload runtime after subscription refresh: %w", err)
	}
	return nil
}

func UpdateSubscription(ctx context.Context, id uint, input SubscriptionUpdateInput) (sub *db.Subscription, err error) {
	if input.Tag != nil && *input.Tag != "" {
		if err = common.ValidateTag(*input.Tag); err != nil {
			return nil, err
		}
	}

	tx := db.BeginTx(ctx)
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	var current db.Subscription
	if err = tx.Where(&db.Subscription{ID: id}).First(&current).Error; err != nil {
		return nil, err
	}

	updated := map[string]any{}
	cronChanged := false
	targetCronEnable := current.CronEnable

	if input.Link != nil {
		updated["link"] = *input.Link
		updated["updated_at"] = time.Now()
	}
	if input.Tag != nil {
		if *input.Tag == "" {
			updated["tag"] = nil
		} else {
			updated["tag"] = *input.Tag
		}
	}
	if input.CronExp != nil {
		updated["cron_exp"] = *input.CronExp
		cronChanged = true
	}
	if input.CronEnable != nil {
		updated["cron_enable"] = *input.CronEnable
		targetCronEnable = *input.CronEnable
		cronChanged = true
	}

	if cronChanged {
		targetCronExp := current.CronExp
		if input.CronExp != nil {
			targetCronExp = *input.CronExp
		}
		if err = validateSubscriptionCron(targetCronExp, targetCronEnable); err != nil {
			return nil, err
		}
	}

	if len(updated) == 0 {
		return &current, nil
	}
	if err = tx.Model(&current).
		Clauses(clause.Returning{}).
		Updates(updated).Error; err != nil {
		return nil, err
	}
	if input.Link != nil {
		if err = autoUpdateGroupVersionsBySubscriptionIDs(tx, []uint{id}); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit().Error; err != nil {
		return nil, err
	}

	if cronChanged {
		RemoveSubscriptionUpdateScheduler(id)
		if targetCronEnable {
			AddSubscriptionUpdateScheduler(ctx, id)
		}
	}
	return &current, nil
}

func DeleteSubscription(ctx context.Context, id uint) (deleted bool, err error) {
	count, err := DeleteSubscriptions(ctx, []uint{id})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func DeleteSubscriptions(ctx context.Context, ids []uint) (count int32, err error) {
	if len(ids) == 0 {
		return 0, nil
	}

	tx := db.BeginTx(ctx)
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	var nodes []db.Node
	if err = tx.Where("subscription_id in ?", ids).Find(&nodes).Error; err != nil {
		return 0, err
	}
	nodeIDs := make([]uint, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}

	if err = autoUpdateGroupVersionsByNodeIDs(tx, nodeIDs); err != nil {
		return 0, err
	}
	if err = autoUpdateGroupVersionsBySubscriptionIDs(tx, ids); err != nil {
		return 0, err
	}
	if err = deleteNodeLatencyResultsWithTx(tx, nodeIDs); err != nil {
		return 0, err
	}
	if err = tx.Where("subscription_id in ?", ids).Delete(&db.Node{}).Error; err != nil {
		return 0, err
	}
	if err = tx.Where("subscription_id in ?", ids).Delete(&db.GroupSubscription{}).Error; err != nil {
		return 0, err
	}
	q := tx.Where("id in ?", ids).
		Select(clause.Associations).
		Delete(&db.Subscription{})
	if q.Error != nil {
		return 0, q.Error
	}
	if err = tx.Commit().Error; err != nil {
		return 0, err
	}
	removeNodeLatencyResults(nodeIDs)
	for _, id := range ids {
		RemoveSubscriptionUpdateScheduler(id)
	}
	return int32(q.RowsAffected), nil
}

func ListSubscriptions(ctx context.Context, id *uint) ([]db.Subscription, error) {
	q := db.DB(ctx).Model(&db.Subscription{})
	if id != nil {
		q = q.Where("id = ?", *id)
	}
	var models []db.Subscription
	if err := q.Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func GetSubscription(ctx context.Context, id uint) (*db.Subscription, error) {
	var sub db.Subscription
	if err := db.DB(ctx).First(&sub, id).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func EnsureSubscriptionSchedulers(ctx context.Context) {
	var subs []db.Subscription
	if err := db.DB(ctx).Find(&subs).Error; err != nil {
		logrus.Error(err)
		return
	}
	for _, sub := range subs {
		AddSubscriptionUpdateScheduler(ctx, sub.ID)
	}
}

func AddSubscriptionUpdateScheduler(ctx context.Context, id uint) {
	subscriptionSchedulerMu.Lock()
	defer subscriptionSchedulerMu.Unlock()

	var sub db.Subscription
	if err := db.DB(ctx).Where("id = ?", id).First(&sub).Error; err != nil {
		logrus.Error(err)
		return
	}
	if !sub.CronEnable {
		return
	}
	s := ensureSubscriptionSchedulerLocked()
	tagName := subscriptionSchedulerTag(sub.ID)
	if jobs, err := s.FindJobsByTag(tagName); err == nil && len(jobs) > 0 {
		return
	}
	tag := "unnamed"
	if sub.Tag != nil {
		tag = *sub.Tag
	}
	logrus.Info("Subscription " + tag + " update task enabled, with exp " + sub.CronExp)
	_, err := s.Tag(tagName).Cron(sub.CronExp).SingletonMode().Do(func() {
		if _, refreshErr := RefreshSubscription(context.Background(), sub.ID); refreshErr != nil {
			logrus.Error(refreshErr)
		}
	})
	if err != nil {
		logrus.Errorf("Failed to schedule subscription %d update: invalid cron expression '%s': %v", sub.ID, sub.CronExp, err)
		return
	}
}

func RemoveSubscriptionUpdateScheduler(id uint) {
	subscriptionSchedulerMu.Lock()
	defer subscriptionSchedulerMu.Unlock()

	if subscriptionScheduler == nil {
		return
	}

	logrus.Info(fmt.Sprintf("Subscription %d update task disabled", id))
	if err := subscriptionScheduler.RemoveByTag(subscriptionSchedulerTag(id)); err != nil && err != gocron.ErrJobNotFoundWithTag {
		logrus.Error(err)
	}
}

func ensureSubscriptionSchedulerLocked() *gocron.Scheduler {
	if subscriptionScheduler != nil {
		return subscriptionScheduler
	}
	subscriptionScheduler = gocron.NewScheduler(time.Local)
	subscriptionScheduler.StartAsync()
	return subscriptionScheduler
}

func subscriptionSchedulerTag(id uint) string {
	return fmt.Sprintf("subscription:%d", id)
}

func autoUpdateGroupVersionsBySubscriptionIDs(d *gorm.DB, ids []uint) error {
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
                    inner join group_subscriptions
                    on g.system_id = ? and g.id = group_subscriptions.group_id and group_subscriptions.subscription_id in ?
				where g.id = groups.id`, sys.ID, ids).Error
}

func hasAnyImportedNode(results []*NodeImportResult) bool {
	for _, result := range results {
		if result != nil && result.Error == nil {
			return true
		}
	}
	return false
}

func validateSubscriptionCron(cronExp string, cronEnable bool) error {
	if !cronEnable || cronExp == "" {
		return nil
	}
	s := gocron.NewScheduler(time.Local)
	_, err := s.Cron(cronExp).Do(func() {})
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", cronExp, err)
	}
	s.Stop()
	return nil
}
