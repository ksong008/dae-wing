/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package config

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/dae"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/graphql/service/config/global"
	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/pkg/config_parser"
	"github.com/graph-gophers/graphql-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func Create(ctx context.Context, name string, glob *global.Input) (*Resolver, error) {
	if glob == nil {
		glob = &global.Input{}
	}
	strGlobal, err := glob.Marshal()
	if err != nil {
		return nil, err
	}
	m := db.Config{
		ID:       0,
		Name:     name,
		Global:   strGlobal,
		Selected: false,
	}
	// Check grammar and to dae config.
	c, err := dae.ParseConfig(&m.Global, nil, nil)
	if err != nil {
		return nil, err
	}
	if err = db.DB(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	return &Resolver{
		DaeGlobal: &c.Global,
		Model:     &m,
	}, nil
}

func Update(ctx context.Context, _id graphql.ID, inputGlobal global.Input) (*Resolver, error) {
	id, err := common.DecodeCursor(_id)
	if err != nil {
		return nil, err
	}
	tx := db.BeginTx(ctx)
	defer func() {
		if finishErr := db.FinishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()
	var m db.Config
	if err = tx.Model(&db.Config{}).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	// Prepare to partially update.
	// Convert global string in database to daeConfig.Global.
	c, err := dae.ParseConfig(&m.Global, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("bad current config: %w", err)
	}
	// Assign input items to daeConfig.Global.
	inputGlobal.Assign(&c.Global)
	// Marshal back to string.
	marshaller := daeConfig.Marshaller{
		IndentSpace: 2,
		// We do not ignore any zero value because all values are valid.
		// That is to say, `ParseConfig` filled in all the values.
		IgnoreZero: false,
	}
	if err = marshaller.MarshalSection("global", reflect.ValueOf(c.Global), 0); err != nil {
		return nil, err
	}
	// Update.
	if err = tx.Model(&db.Config{ID: id}).Updates(map[string]interface{}{
		"global":  string(marshaller.Bytes()),
		"version": gorm.Expr("version + 1"),
	}).Error; err != nil {
		return nil, err
	}
	return &Resolver{
		DaeGlobal: &c.Global,
		Model:     &m,
	}, nil
}

func Remove(ctx context.Context, _id graphql.ID) (n int32, err error) {
	id, err := common.DecodeCursor(_id)
	if err != nil {
		return 0, err
	}
	tx := db.BeginTx(ctx)
	defer func() {
		if finishErr := db.FinishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()
	m := db.Config{ID: id}
	q := tx.Clauses(clause.Returning{Columns: []clause.Column{{Name: "selected"}}}).
		Select(clause.Associations).
		Delete(&m)
	if q.Error != nil {
		return 0, q.Error
	}
	return int32(q.RowsAffected), nil
}

type node struct {
	dbNode     *db.Node
	groups     []*db.Group
	uniqueName string
}

func matchedNodesForGroupSubscription(binding *db.GroupSubscription) ([]db.Node, error) {
	nodes := binding.Subscription.Node
	if binding.NameFilterRegex == nil || *binding.NameFilterRegex == "" {
		return nodes, nil
	}

	re, err := regexp.Compile(*binding.NameFilterRegex)
	if err != nil {
		return nil, err
	}

	var matched []db.Node
	for _, n := range nodes {
		if re.MatchString(n.Name) {
			matched = append(matched, n)
		}
	}
	return matched, nil
}

func deduplicateNodes(nodes []*node) []*node {
	set := make(map[string]*node)
	for _, node := range nodes {
		if oldNode, ok := set[node.dbNode.Link]; ok {
			oldNode.groups = append(oldNode.groups, node.groups...)
		} else {
			set[node.dbNode.Link] = node
		}
	}
	ret := make([]*node, 0, len(set))
	for _, node := range set {
		ret = append(ret, node)
	}
	return ret
}

// normNodeName normalize the name to satify the "key" format in dae config.
func normNodeName(_name string) string {
	name := []rune(_name)
	ret := make([]rune, 0, len(name))
	for _, r := range name {
		if r == ':' || r == '\'' {
			r = '_'
		}
		ret = append(ret, r)
	}
	return string(ret)
}

func uniquefyNodesName(nodes []*node) {
	// Uniquefy names of nodes.
	// Sort nodes by "has node.Tag" because node.Tag is unique but names of others may be the same with them.
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].dbNode.Tag != nil && nodes[j].dbNode.Tag == nil
	})
	nameToNodes := make(map[string]*node)
	for i := range nodes {
		node := nodes[i]
		if node.dbNode.Tag != nil {
			nameToNodes[*node.dbNode.Tag] = node
		} else {
			baseName := normNodeName(node.dbNode.Name)
			if node.dbNode.SubscriptionID != nil {
				baseName = fmt.Sprintf("%v.%v", *node.dbNode.SubscriptionID, baseName)
			}
			// SubID.Name
			wantedName := baseName
			for j := 0; ; j++ {
				_, exist := nameToNodes[wantedName]
				if !exist {
					nameToNodes[wantedName] = node
					break
				}
				// SubID.Name.1
				wantedName = fmt.Sprintf("%v.%v", baseName, j)
			}
		}
	}
	for name, node := range nameToNodes {
		node.uniqueName = name
	}
}

func Select(ctx context.Context, _id graphql.ID) (n int32, err error) {
	id, err := common.DecodeCursor(_id)
	if err != nil {
		return 0, err
	}
	tx := db.BeginTx(ctx)
	defer func() {
		if finishErr := db.FinishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()
	// Unset all selected.
	q := tx.Model(&db.Config{}).Where("selected = ?", true).Update("selected", false)
	if err = q.Error; err != nil {
		return 0, err
	}
	// Set selected.
	q = tx.Model(&db.Config{ID: id}).Update("selected", true)
	if err = q.Error; err != nil {
		return 0, err
	}
	if q.RowsAffected == 0 {
		return 0, fmt.Errorf("no such config")
	}
	return 1, nil
}

func Rename(ctx context.Context, _id graphql.ID, name string) (n int32, err error) {
	id, err := common.DecodeCursor(_id)
	if err != nil {
		return 0, err
	}
	q := db.DB(ctx).Model(&db.Config{ID: id}).
		Update("name", name)
	if q.Error != nil {
		return 0, q.Error
	}
	return int32(q.RowsAffected), nil
}

var runLock sync.Mutex

const reloadCallbackTimeout = 2 * time.Minute

func reloadWithContext(ctx context.Context, cfg *daeConfig.Config) error {
	chReloadCallback := make(chan error, 1)
	msg := &dae.ReloadMessage{
		Config:   cfg,
		Callback: chReloadCallback,
	}
	select {
	case dae.ChReloadConfigs <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}

	waitCtx, cancel := context.WithTimeout(ctx, reloadCallbackTimeout)
	defer cancel()
	select {
	case err := <-chReloadCallback:
		return err
	case <-waitCtx.Done():
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("reload timed out after %s", reloadCallbackTimeout)
		}
		return waitCtx.Err()
	}
}

func Run(ctx context.Context, noLoad bool) (n int32, err error) {
	if ok := runLock.TryLock(); !ok {
		return 0, fmt.Errorf("the last request didn't complete; make a cup of tea and take a break")
	}
	defer runLock.Unlock()

	state, err := prepareRunState(ctx, noLoad)
	if err != nil {
		return 0, err
	}

	if err = reloadWithContext(ctx, state.config); err != nil {
		if noLoad {
			return 0, fmt.Errorf("failed to dryrun: %w; see more in log and report bugs", err)
		}
		return 0, fmt.Errorf("failed to load new config: %w; see more in log", err)
	}

	persistCtx := context.Background()
	if ctx != nil {
		persistCtx = context.WithoutCancel(ctx)
	}
	tx := db.BeginTx(persistCtx)
	if tx.Error != nil {
		return 0, tx.Error
	}
	defer func() {
		if finishErr := db.FinishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()

	return state.persist(tx)
}

type runState struct {
	config                *daeConfig.Config
	running               bool
	runningConfigID       uint
	runningConfigVersion  uint
	runningDnsID          uint
	runningDnsVersion     uint
	runningRoutingID      uint
	runningRoutingVersion uint
	runningGroupVersion   uint
	runningGroupIDs       []string
	runningGroups         []db.Group
}

func (s *runState) persist(d *gorm.DB) (n int32, err error) {
	var sys db.System
	if err = d.Model(&db.System{}).FirstOrCreate(&sys).Error; err != nil {
		return 0, err
	}
	if !s.running {
		if err = d.Model(&sys).Updates(map[string]interface{}{
			"running": false,
		}).Error; err != nil {
			return 0, err
		}
		return 1, nil
	}
	if err = d.Model(&sys).Updates(map[string]interface{}{
		"running":                   true,
		"running_config_id":         s.runningConfigID,
		"running_config_version":    s.runningConfigVersion,
		"running_dns_id":            s.runningDnsID,
		"running_dns_version":       s.runningDnsVersion,
		"running_routing_id":        s.runningRoutingID,
		"running_routing_version":   s.runningRoutingVersion,
		"running_group_version_sum": s.runningGroupVersion,
		"running_group_ids":         strings.Join(s.runningGroupIDs, ","),
	}).Error; err != nil {
		return 0, err
	}
	if err = d.Model(&sys).Association("RunningGroups").Replace(s.runningGroups); err != nil {
		return 0, err
	}
	return 1, nil
}

func prepareRunState(ctx context.Context, noLoad bool) (state *runState, err error) {
	if noLoad {
		return &runState{
			config:  dae.EmptyConfig,
			running: false,
		}, nil
	}

	tx := db.BeginReadOnlyTx(ctx)
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if finishErr := db.FinishTx(tx, err); finishErr != nil {
			err = finishErr
		}
	}()

	return prepareRunStateWithTx(tx)
}

func prepareRunStateWithTx(d *gorm.DB) (state *runState, err error) {

	//// Run selected global+dns+routing.
	/// Get them from database and parse them to daeConfig.
	var mConfig db.Config
	var mDns db.Dns
	var mRouting db.Routing
	q := d.Model(&db.Config{}).Where("selected = ?", true).First(&mConfig)
	if (q.Error == nil && q.RowsAffected == 0) || errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("please select a config")
	}
	if q.Error != nil {
		return nil, q.Error
	}
	q = d.Model(&db.Dns{}).Where("selected = ?", true).First(&mDns)
	if (q.Error == nil && q.RowsAffected == 0) || errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("please select a dns")
	}
	if q.Error != nil {
		return nil, q.Error
	}
	q = d.Model(&db.Routing{}).Where("selected = ?", true).First(&mRouting)
	if (q.Error == nil && q.RowsAffected == 0) || errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("please select a routing")
	}
	if q.Error != nil {
		return nil, q.Error
	}
	c, err := dae.ParseConfig(&mConfig.Global, &mDns.Dns, &mRouting.Routing)
	if err != nil {
		return nil, err
	}
	/// Fill in necessary groups and nodes.
	// Find groups needed by routing.
	outbounds := dae.NecessaryOutbounds(&c.Routing)
	var groups []db.Group
	q = d.Model(&db.Group{}).
		Where("name in ?", outbounds).
		Preload("PolicyParams").
		Preload("SubscriptionBindings").
		Preload("SubscriptionBindings.Subscription").
		Preload("SubscriptionBindings.Subscription.Node").
		Find(&groups)
	if q.Error != nil {
		return nil, q.Error
	}

	{
		// Find not found.
		nameSet := map[string]struct{}{}
		for _, name := range outbounds {
			nameSet[name] = struct{}{}
		}
		for _, g := range groups {
			delete(nameSet, g.Name)
		}
		var notFound []string
		for name := range nameSet {
			switch name {
			case "direct", "block", "must_rules":
				// Preset groups.
			default:
				notFound = append(notFound, name)
			}
		}
		if len(notFound) > 0 {
			return nil, fmt.Errorf("groups not defined but referenced by routing: %v", strings.Join(notFound, ", "))
		}
	}
	// Find nodes in groups.
	var nodes []*node
	for i := range groups {
		for _, binding := range groups[i].SubscriptionBindings {
			matchedNodes, err := matchedNodesForGroupSubscription(&binding)
			if err != nil {
				subscriptionName := binding.Subscription.Link
				if binding.Subscription.Tag != nil && *binding.Subscription.Tag != "" {
					subscriptionName = *binding.Subscription.Tag
				}
				return nil, fmt.Errorf("group '%v' has invalid subscription regex for '%v': %w", groups[i].Name, subscriptionName, err)
			}
			for _, n := range matchedNodes {
				n := n
				nodes = append(nodes, &node{
					dbNode: &n,
					groups: []*db.Group{&groups[i]},
				})
			}
		}
		var solitaryNodes []db.Node
		if err = d.Model(groups[i]).
			Association("Node").
			Find(&solitaryNodes); err != nil {
			return nil, err
		}
		for _, n := range solitaryNodes {
			n := n
			nodes = append(nodes, &node{
				dbNode: &n,
				groups: []*db.Group{&groups[i]},
			})
		}
	}
	nodes = deduplicateNodes(nodes)
	uniquefyNodesName(nodes)
	// Group -> nodes
	mGroupNode := make(map[*db.Group]map[*node]struct{})
	for i := range groups {
		mGroupNode[&groups[i]] = make(map[*node]struct{})
	}
	for _, n := range nodes {
		for _, group := range n.groups {
			mGroupNode[group][n] = struct{}{}
		}
	}
	// Fill in group section.
	for g, sNodes := range mGroupNode {
		if len(sNodes) == 0 {
			return nil, fmt.Errorf("please add at least one node into group '%v' (referenced by current routing '%v')", g.Name, mRouting.Name)
		}
		// Parse policy.
		var policy daeConfig.FunctionListOrString
		if len(g.PolicyParams) == 0 {
			policy = g.Policy
		} else {
			// Parse policy params.
			var params []*config_parser.Param
			for _, param := range g.PolicyParams {
				params = append(params, param.Marshal())
			}
			policy = &config_parser.Function{
				Name:   g.Policy,
				Not:    false,
				Params: params,
			}
		}
		// fiexed group cannot have more than one node.
		if g.Policy == "fixed" && len(sNodes) > 1 {
			return nil, fmt.Errorf("group '%v' with policy 'fixed' cannot have more than one node", g.Name)
		}
		// Node names to filter.
		var names []*config_parser.Param
		for node := range sNodes {
			names = append(names, &config_parser.Param{
				Val: node.uniqueName,
			})
		}
		grp := daeConfig.Group{
			Name: g.Name,
			Filter: [][]*config_parser.Function{{{
				Name:   "name",
				Not:    false,
				Params: names,
			}}},
			Policy: policy,
		}
		for range grp.Filter {
			// Keep the same length as filter's.
			grp.FilterAnnotation = append(grp.FilterAnnotation, []*config_parser.Param{})
		}
		c.Group = append(c.Group, grp)
	}
	// Fill in node section.
	for _, node := range nodes {
		c.Node = append(c.Node, daeConfig.KeyableString(fmt.Sprintf("%v:%v", node.uniqueName, node.dbNode.Link)))
	}

	var gvs uint
	var gids []string
	for _, g := range groups {
		gvs += g.Version
		gids = append(gids, fmt.Sprintf("%x", g.ID))
	}
	sort.Slice(gids, func(i, j int) bool {
		return gids[i] < gids[j]
	})

	return &runState{
		config:                c,
		running:               true,
		runningConfigID:       mConfig.ID,
		runningConfigVersion:  mConfig.Version,
		runningDnsID:          mDns.ID,
		runningDnsVersion:     mDns.Version,
		runningRoutingID:      mRouting.ID,
		runningRoutingVersion: mRouting.Version,
		runningGroupVersion:   gvs,
		runningGroupIDs:       gids,
		runningGroups:         groups,
	}, nil
}
