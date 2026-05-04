/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/orchestrator"
	"gorm.io/gorm"
)

const daeBundleSchemaVersion = 1

type daeBundle struct {
	SchemaVersion int                `json:"schemaVersion"`
	ExportedAt    string             `json:"exportedAt"`
	Mode          string             `json:"mode"`
	Defaults      daeBundleDefaults  `json:"defaults"`
	Selected      daeBundleSelected  `json:"selected"`
	Configs       []daeBundleConfig  `json:"configs"`
	DNSS          []daeBundleDNS     `json:"dnss"`
	Routings      []daeBundleRouting `json:"routings"`
	Subscriptions []daeBundleSub     `json:"subscriptions"`
	Nodes         []daeBundleNode    `json:"nodes"`
	Groups        []daeBundleGroup   `json:"groups"`
}

type daeBundleDefaults struct {
	ConfigID  *uint `json:"configId,omitempty"`
	DNSID     *uint `json:"dnsId,omitempty"`
	RoutingID *uint `json:"routingId,omitempty"`
	GroupID   *uint `json:"groupId,omitempty"`
}

type daeBundleSelected struct {
	ConfigID  *uint `json:"configId,omitempty"`
	DNSID     *uint `json:"dnsId,omitempty"`
	RoutingID *uint `json:"routingId,omitempty"`
}

type daeBundleConfig struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Global string `json:"global"`
}

type daeBundleDNS struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	DNS  string `json:"dns"`
}

type daeBundleRouting struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Routing string `json:"routing"`
}

type daeBundleSub struct {
	ID         uint    `json:"id"`
	UpdatedAt  string  `json:"updatedAt"`
	Link       string  `json:"link"`
	CronExp    string  `json:"cronExp"`
	CronEnable bool    `json:"cronEnable"`
	Status     string  `json:"status"`
	Info       string  `json:"info"`
	Tag        *string `json:"tag,omitempty"`
}

type daeBundleNode struct {
	ID             uint    `json:"id"`
	Link           string  `json:"link"`
	Name           string  `json:"name"`
	Address        string  `json:"address"`
	Protocol       string  `json:"protocol"`
	Tag            *string `json:"tag,omitempty"`
	SubscriptionID *uint   `json:"subscriptionId,omitempty"`
}

type daeBundleGroup struct {
	ID                   uint                         `json:"id"`
	Name                 string                       `json:"name"`
	Policy               string                       `json:"policy"`
	PolicyParams         []paramResource              `json:"policyParams"`
	NodeIDs              []uint                       `json:"nodeIds"`
	SubscriptionBindings []daeBundleGroupSubscription `json:"subscriptionBindings"`
}

type daeBundleGroupSubscription struct {
	SubscriptionID  uint    `json:"subscriptionId"`
	NameFilterRegex *string `json:"nameFilterRegex,omitempty"`
}

func handleCurrentUserDAEBundle(rw http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromContext(r)
	if !ok {
		writeError(rw, http.StatusUnauthorized, "authentication required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		bundle, err := exportDAEBundle(r.Context(), user)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, bundle)
	case http.MethodPut:
		var bundle daeBundle
		if err := decodeJSONBody(r, &bundle); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if err := importDAEBundle(r.Context(), user, &bundle); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"imported": true})
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut)
	}
}

func exportDAEBundle(ctx context.Context, user *db.User) (*daeBundle, error) {
	bundle := &daeBundle{
		SchemaVersion: daeBundleSchemaVersion,
		ExportedAt:    time.Now().Format(time.RFC3339Nano),
	}

	stored := orchestrator.QueryJSONStorage(user, []string{"defaultConfigID", "defaultRoutingID", "defaultDNSID", "defaultGroupID", "mode"})
	if len(stored) >= 5 {
		bundle.Defaults.ConfigID = storedUintPtr(stored[0])
		bundle.Defaults.RoutingID = storedUintPtr(stored[1])
		bundle.Defaults.DNSID = storedUintPtr(stored[2])
		bundle.Defaults.GroupID = storedUintPtr(stored[3])
		bundle.Mode = stored[4]
	}

	var configs []db.Config
	if err := db.DB(ctx).Order("id asc").Find(&configs).Error; err != nil {
		return nil, err
	}
	for _, model := range configs {
		bundle.Configs = append(bundle.Configs, daeBundleConfig{
			ID:     model.ID,
			Name:   model.Name,
			Global: model.Global,
		})
		if model.Selected {
			id := model.ID
			bundle.Selected.ConfigID = &id
		}
	}

	var dnss []db.Dns
	if err := db.DB(ctx).Order("id asc").Find(&dnss).Error; err != nil {
		return nil, err
	}
	for _, model := range dnss {
		bundle.DNSS = append(bundle.DNSS, daeBundleDNS{
			ID:   model.ID,
			Name: model.Name,
			DNS:  model.Dns,
		})
		if model.Selected {
			id := model.ID
			bundle.Selected.DNSID = &id
		}
	}

	var routings []db.Routing
	if err := db.DB(ctx).Order("id asc").Find(&routings).Error; err != nil {
		return nil, err
	}
	for _, model := range routings {
		bundle.Routings = append(bundle.Routings, daeBundleRouting{
			ID:      model.ID,
			Name:    model.Name,
			Routing: model.Routing,
		})
		if model.Selected {
			id := model.ID
			bundle.Selected.RoutingID = &id
		}
	}

	var subscriptions []db.Subscription
	if err := db.DB(ctx).Order("id asc").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	for _, model := range subscriptions {
		bundle.Subscriptions = append(bundle.Subscriptions, daeBundleSub{
			ID:         model.ID,
			UpdatedAt:  model.UpdatedAt.Format(time.RFC3339Nano),
			Link:       model.Link,
			CronExp:    model.CronExp,
			CronEnable: model.CronEnable,
			Status:     model.Status,
			Info:       model.Info,
			Tag:        model.Tag,
		})
	}

	var nodes []db.Node
	if err := db.DB(ctx).Order("id asc").Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, model := range nodes {
		bundle.Nodes = append(bundle.Nodes, daeBundleNode{
			ID:             model.ID,
			Link:           model.Link,
			Name:           model.Name,
			Address:        model.Address,
			Protocol:       model.Protocol,
			Tag:            model.Tag,
			SubscriptionID: model.SubscriptionID,
		})
	}

	groups, err := orchestrator.ListGroups(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	for _, model := range groups {
		group := daeBundleGroup{
			ID:                   model.ID,
			Name:                 model.Name,
			Policy:               model.Policy,
			PolicyParams:         make([]paramResource, 0, len(model.PolicyParams)),
			NodeIDs:              make([]uint, 0, len(model.Node)),
			SubscriptionBindings: make([]daeBundleGroupSubscription, 0, len(model.SubscriptionBindings)),
		}
		for _, param := range model.PolicyParams {
			group.PolicyParams = append(group.PolicyParams, paramResource{
				Key: param.Key,
				Val: param.Value,
			})
		}
		for _, node := range model.Node {
			group.NodeIDs = append(group.NodeIDs, node.ID)
		}
		for _, binding := range model.SubscriptionBindings {
			group.SubscriptionBindings = append(group.SubscriptionBindings, daeBundleGroupSubscription{
				SubscriptionID:  binding.SubscriptionID,
				NameFilterRegex: binding.NameFilterRegex,
			})
		}
		bundle.Groups = append(bundle.Groups, group)
	}

	return bundle, nil
}

func importDAEBundle(ctx context.Context, user *db.User, bundle *daeBundle) error {
	if bundle.SchemaVersion != daeBundleSchemaVersion {
		return fmt.Errorf("unsupported bundle schema version: %d", bundle.SchemaVersion)
	}

	var oldSubscriptions []db.Subscription
	if err := db.DB(ctx).Select("id").Find(&oldSubscriptions).Error; err != nil {
		return err
	}
	oldSubscriptionIDs := make([]uint, 0, len(oldSubscriptions))
	for _, sub := range oldSubscriptions {
		oldSubscriptionIDs = append(oldSubscriptionIDs, sub.ID)
	}

	tx := db.BeginTx(ctx)
	_, _, _, _, subIDMap, err := replaceDAEBundleResources(tx, user, bundle)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}

	for _, oldID := range oldSubscriptionIDs {
		orchestrator.RemoveSubscriptionUpdateScheduler(oldID)
	}
	for _, newID := range subIDMap {
		orchestrator.AddSubscriptionUpdateScheduler(ctx, newID)
	}
	return nil
}

func replaceDAEBundleResources(tx *gorm.DB, user *db.User, bundle *daeBundle) (
	map[uint]uint,
	map[uint]uint,
	map[uint]uint,
	map[uint]uint,
	map[uint]uint,
	error,
) {
	if err := tx.Exec("DELETE FROM group_nodes").Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Exec("DELETE FROM group_subscriptions").Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Exec("DELETE FROM group_policy_params").Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Group{}).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Node{}).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Subscription{}).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Config{}).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Dns{}).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := tx.Where("1 = 1").Delete(&db.Routing{}).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}

	configIDMap := make(map[uint]uint, len(bundle.Configs))
	createdConfigIDs := make([]uint, 0, len(bundle.Configs))
	for _, item := range bundle.Configs {
		model := db.Config{Name: item.Name, Global: normalizeSection(&item.Global, "global {}")}
		if _, err := engine.Default().ParseConfig(&model.Global, nil, nil); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("config %q: %w", item.Name, err)
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, nil, nil, nil, nil, err
		}
		configIDMap[item.ID] = model.ID
		createdConfigIDs = append(createdConfigIDs, model.ID)
	}

	dnsIDMap := make(map[uint]uint, len(bundle.DNSS))
	createdDNSIDs := make([]uint, 0, len(bundle.DNSS))
	for _, item := range bundle.DNSS {
		model := db.Dns{Name: item.Name, Dns: normalizeEnsureSection("dns", item.DNS, engine.Default().EmptyDnsSection())}
		if _, err := engine.Default().ParseConfig(nil, &model.Dns, nil); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("dns %q: %w", item.Name, err)
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, nil, nil, nil, nil, err
		}
		dnsIDMap[item.ID] = model.ID
		createdDNSIDs = append(createdDNSIDs, model.ID)
	}

	routingIDMap := make(map[uint]uint, len(bundle.Routings))
	createdRoutingIDs := make([]uint, 0, len(bundle.Routings))
	for _, item := range bundle.Routings {
		model := db.Routing{Name: item.Name, Routing: normalizeEnsureSection("routing", item.Routing, engine.Default().EmptyRoutingSection())}
		if _, err := engine.Default().ParseConfig(nil, nil, &model.Routing); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("routing %q: %w", item.Name, err)
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, nil, nil, nil, nil, err
		}
		routingIDMap[item.ID] = model.ID
		createdRoutingIDs = append(createdRoutingIDs, model.ID)
	}

	subIDMap := make(map[uint]uint, len(bundle.Subscriptions))
	for _, item := range bundle.Subscriptions {
		updatedAt, err := time.Parse(time.RFC3339Nano, item.UpdatedAt)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("subscription %q updatedAt: %w", item.Link, err)
		}
		model := db.Subscription{
			UpdatedAt:  updatedAt,
			Link:       item.Link,
			CronExp:    item.CronExp,
			CronEnable: item.CronEnable,
			Status:     item.Status,
			Info:       item.Info,
			Tag:        item.Tag,
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, nil, nil, nil, nil, err
		}
		subIDMap[item.ID] = model.ID
	}

	nodeIDMap := make(map[uint]uint, len(bundle.Nodes))
	for _, item := range bundle.Nodes {
		var subscriptionID *uint
		if item.SubscriptionID != nil {
			mappedID, ok := subIDMap[*item.SubscriptionID]
			if !ok {
				return nil, nil, nil, nil, nil, fmt.Errorf("node %q references missing subscription %d", item.Name, *item.SubscriptionID)
			}
			subscriptionID = &mappedID
		}
		model := db.Node{
			Link:           item.Link,
			Name:           item.Name,
			Address:        item.Address,
			Protocol:       item.Protocol,
			Tag:            item.Tag,
			SubscriptionID: subscriptionID,
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, nil, nil, nil, nil, err
		}
		nodeIDMap[item.ID] = model.ID
	}

	groupIDMap := make(map[uint]uint, len(bundle.Groups))
	for _, item := range bundle.Groups {
		params := make([]db.GroupPolicyParam, len(item.PolicyParams))
		for i := range params {
			params[i] = db.GroupPolicyParam{
				Key:   item.PolicyParams[i].Key,
				Value: item.PolicyParams[i].Val,
			}
		}
		model := db.Group{
			Name:         item.Name,
			Policy:       item.Policy,
			PolicyParams: params,
		}
		if err := tx.Create(&model).Error; err != nil {
			return nil, nil, nil, nil, nil, err
		}
		groupIDMap[item.ID] = model.ID

		nodes := make([]db.Node, 0, len(item.NodeIDs))
		for _, oldNodeID := range item.NodeIDs {
			newNodeID, ok := nodeIDMap[oldNodeID]
			if !ok {
				return nil, nil, nil, nil, nil, fmt.Errorf("group %q references missing node %d", item.Name, oldNodeID)
			}
			nodes = append(nodes, db.Node{ID: newNodeID})
		}
		if len(nodes) > 0 {
			if err := tx.Model(&model).Association("Node").Append(nodes); err != nil {
				return nil, nil, nil, nil, nil, err
			}
		}
		for _, binding := range item.SubscriptionBindings {
			newSubID, ok := subIDMap[binding.SubscriptionID]
			if !ok {
				return nil, nil, nil, nil, nil, fmt.Errorf("group %q references missing subscription %d", item.Name, binding.SubscriptionID)
			}
			if err := tx.Create(&db.GroupSubscription{
				GroupID:         model.ID,
				SubscriptionID:  newSubID,
				NameFilterRegex: binding.NameFilterRegex,
			}).Error; err != nil {
				return nil, nil, nil, nil, nil, err
			}
		}
	}

	selectedConfigID, err := resolveMappedSelection(bundle.Selected.ConfigID, bundle.Defaults.ConfigID, configIDMap, createdConfigIDs)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := applySelectedResource(tx, &db.Config{}, selectedConfigID); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	selectedDNSID, err := resolveMappedSelection(bundle.Selected.DNSID, bundle.Defaults.DNSID, dnsIDMap, createdDNSIDs)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := applySelectedResource(tx, &db.Dns{}, selectedDNSID); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	selectedRoutingID, err := resolveMappedSelection(bundle.Selected.RoutingID, bundle.Defaults.RoutingID, routingIDMap, createdRoutingIDs)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := applySelectedResource(tx, &db.Routing{}, selectedRoutingID); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	defaultConfigID, err := resolveMappedDefault(bundle.Defaults.ConfigID, selectedConfigID, configIDMap, createdConfigIDs)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	defaultDNSID, err := resolveMappedDefault(bundle.Defaults.DNSID, selectedDNSID, dnsIDMap, createdDNSIDs)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	defaultRoutingID, err := resolveMappedDefault(bundle.Defaults.RoutingID, selectedRoutingID, routingIDMap, createdRoutingIDs)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	defaultGroupID, err := resolveMappedDefault(bundle.Defaults.GroupID, nil, groupIDMap, groupMapValuesInOrder(bundle.Groups, groupIDMap))
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if err := setJSONStorageWithTx(tx, user, []string{"defaultConfigID", "defaultRoutingID", "defaultDNSID", "defaultGroupID", "mode"}, []string{
		formatOptionalUint(defaultConfigID),
		formatOptionalUint(defaultRoutingID),
		formatOptionalUint(defaultDNSID),
		formatOptionalUint(defaultGroupID),
		bundle.Mode,
	}); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	return configIDMap, dnsIDMap, routingIDMap, groupIDMap, subIDMap, nil
}

func applySelectedResource(tx *gorm.DB, model any, selectedID *uint) error {
	if err := tx.Model(model).Where("selected = ?", true).Update("selected", false).Error; err != nil {
		return err
	}
	if selectedID == nil {
		return nil
	}
	return tx.Model(model).Where("id = ?", *selectedID).Update("selected", true).Error
}

func resolveMappedSelection(preferred *uint, fallback *uint, mapping map[uint]uint, orderedIDs []uint) (*uint, error) {
	if preferred != nil {
		mappedID, ok := mapping[*preferred]
		if !ok {
			return nil, fmt.Errorf("selected resource %d is missing from bundle payload", *preferred)
		}
		return &mappedID, nil
	}
	if fallback != nil {
		mappedID, ok := mapping[*fallback]
		if !ok {
			return nil, fmt.Errorf("default resource %d is missing from bundle payload", *fallback)
		}
		return &mappedID, nil
	}
	if len(orderedIDs) == 0 {
		return nil, nil
	}
	return &orderedIDs[0], nil
}

func resolveMappedDefault(preferred *uint, fallback *uint, mapping map[uint]uint, orderedIDs []uint) (*uint, error) {
	if preferred != nil {
		mappedID, ok := mapping[*preferred]
		if !ok {
			return nil, fmt.Errorf("default resource %d is missing from bundle payload", *preferred)
		}
		return &mappedID, nil
	}
	if fallback != nil {
		return fallback, nil
	}
	if len(orderedIDs) == 0 {
		return nil, nil
	}
	return &orderedIDs[0], nil
}

func groupMapValuesInOrder(groups []daeBundleGroup, mapping map[uint]uint) []uint {
	values := make([]uint, 0, len(groups))
	for _, group := range groups {
		if mappedID, ok := mapping[group.ID]; ok {
			values = append(values, mappedID)
		}
	}
	return values
}

func formatOptionalUint(value *uint) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func storedUintPtr(value string) *uint {
	parsed, ok := parseStoredUint(value)
	if !ok {
		return nil
	}
	return &parsed
}
