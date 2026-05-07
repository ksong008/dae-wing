/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/pkg/config_parser"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type node struct {
	dbNode     *db.Node
	groups     []*db.Group
	uniqueName string
}

var (
	runtimeLifecycleMu            sync.Mutex
	errRuntimeOperationInProgress = errors.New("the last request didn't complete; make a cup of tea and take a break")
)

func Run(ctx context.Context, dry bool) (n int32, err error) {
	unlock, ok := lockRuntimeLifecycle()
	if !ok {
		return 0, errRuntimeOperationInProgress
	}
	defer unlock()

	return run(ctx, dry)
}

func lockRuntimeLifecycle() (func(), bool) {
	if ok := runtimeLifecycleMu.TryLock(); !ok {
		return nil, false
	}
	return runtimeLifecycleMu.Unlock, true
}

func run(ctx context.Context, dry bool) (n int32, err error) {
	tx := db.BeginTx(ctx)
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()
	if tx.Error != nil {
		return 0, tx.Error
	}
	d := tx

	if dry {
		var sys db.System
		if err = d.Model(&db.System{}).FirstOrCreate(&sys).Error; err != nil {
			return 0, err
		}
		if err = d.Model(&sys).Updates(map[string]interface{}{
			"running": false,
		}).Error; err != nil {
			return 0, err
		}
		if err = tx.Commit().Error; err != nil {
			return 0, err
		}
		committed = true

		err = engine.Default().ReloadWithContext(ctx, engine.Default().EmptyConfig())
		if err != nil {
			return 0, fmt.Errorf("failed to dryrun: %w; see more in log and report bugs", err)
		}
		clearRunningNodeIndex()
		return 1, nil
	}

	var mConfig db.Config
	var mDns db.Dns
	var mRouting db.Routing
	q := d.Model(&db.Config{}).Where("selected = ?", true).First(&mConfig)
	if (q.Error == nil && q.RowsAffected == 0) || errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("please select a config")
	}
	if q.Error != nil {
		return 0, q.Error
	}
	q = d.Model(&db.Dns{}).Where("selected = ?", true).First(&mDns)
	if (q.Error == nil && q.RowsAffected == 0) || errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("please select a dns")
	}
	if q.Error != nil {
		return 0, q.Error
	}
	q = d.Model(&db.Routing{}).Where("selected = ?", true).First(&mRouting)
	if (q.Error == nil && q.RowsAffected == 0) || errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("please select a routing")
	}
	if q.Error != nil {
		return 0, q.Error
	}

	c, err := engine.Default().ParseConfig(&mConfig.Global, &mDns.Dns, &mRouting.Routing)
	if err != nil {
		return 0, err
	}

	outbounds := engine.Default().NecessaryOutbounds(&c.Routing)
	var groups []db.Group
	q = d.Model(&db.Group{}).
		Where("name in ?", outbounds).
		Preload("PolicyParams").
		Preload("SubscriptionBindings").
		Preload("SubscriptionBindings.Subscription").
		Preload("SubscriptionBindings.Subscription.Node").
		Find(&groups)
	if q.Error != nil {
		return 0, q.Error
	}

	{
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
			default:
				notFound = append(notFound, name)
			}
		}
		if len(notFound) > 0 {
			return 0, fmt.Errorf("groups not defined but referenced by routing: %v", strings.Join(notFound, ", "))
		}
	}

	var nodes []*node
	for i := range groups {
		for _, binding := range groups[i].SubscriptionBindings {
			matchedNodes, err := matchedNodesForGroupSubscription(&binding)
			if err != nil {
				subscriptionName := binding.Subscription.Link
				if binding.Subscription.Tag != nil && *binding.Subscription.Tag != "" {
					subscriptionName = *binding.Subscription.Tag
				}
				return 0, fmt.Errorf("group '%v' has invalid subscription regex for '%v': %w", groups[i].Name, subscriptionName, err)
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
			return 0, err
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

	mGroupNode := make(map[*db.Group]map[*node]struct{})
	for i := range groups {
		mGroupNode[&groups[i]] = make(map[*node]struct{})
	}
	for _, n := range nodes {
		for _, group := range n.groups {
			mGroupNode[group][n] = struct{}{}
		}
	}

	for g, sNodes := range mGroupNode {
		if len(sNodes) == 0 {
			return 0, fmt.Errorf("please add at least one node into group '%v' (referenced by current routing '%v')", g.Name, mRouting.Name)
		}
		var policy daeConfig.FunctionListOrString
		if len(g.PolicyParams) == 0 {
			policy = g.Policy
		} else {
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
		if g.Policy == "fixed" && len(sNodes) > 1 {
			return 0, fmt.Errorf("group '%v' with policy 'fixed' cannot have more than one node", g.Name)
		}
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
			grp.FilterAnnotation = append(grp.FilterAnnotation, []*config_parser.Param{})
		}
		c.Group = append(c.Group, grp)
	}
	for _, node := range nodes {
		c.Node = append(c.Node, daeConfig.KeyableString(fmt.Sprintf("%v:%v", node.uniqueName, node.dbNode.Link)))
	}

	var sys db.System
	if err = d.Model(&db.System{}).FirstOrCreate(&sys).Error; err != nil {
		return 0, err
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
	if err = d.Model(&sys).Updates(map[string]interface{}{
		"running":                   true,
		"running_config_id":         mConfig.ID,
		"running_config_version":    mConfig.Version,
		"running_dns_id":            mDns.ID,
		"running_dns_version":       mDns.Version,
		"running_routing_id":        mRouting.ID,
		"running_routing_version":   mRouting.Version,
		"running_group_version_sum": gvs,
		"running_group_ids":         strings.Join(gids, ","),
	}).Error; err != nil {
		return 0, err
	}
	if err = d.Model(&sys).Association("RunningGroups").Replace(groups); err != nil {
		return 0, err
	}
	if err = tx.Commit().Error; err != nil {
		return 0, err
	}
	committed = true

	errReload := engine.Default().ReloadWithContext(ctx, c)
	if errReload != nil {
		clearRunningNodeIndex()
		return 0, markStoppedAfterRestoreFailure(context.WithoutCancel(ctx), fmt.Errorf("failed to load new config: %w; see more in log", errReload))
	}
	replaceRunningNodeIndex(nodes)

	return 1, nil
}

func RestoreRunningState(ctx context.Context) (err error) {
	unlock, ok := lockRuntimeLifecycle()
	if !ok {
		return errRuntimeOperationInProgress
	}
	defer unlock()

	reload, err := shouldRestoreRunningState(ctx)
	if err != nil || !reload {
		return err
	}
	if _, err = run(ctx, false); err != nil {
		return markStoppedAfterRestoreFailure(context.WithoutCancel(ctx), err)
	}
	return nil
}

func Stop(ctx context.Context, timeout time.Duration) (err error) {
	unlock, ok := lockRuntimeLifecycle()
	if !ok {
		return errRuntimeOperationInProgress
	}
	defer unlock()

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	tx := db.BeginTx(ctx)
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()
	if tx.Error != nil {
		return tx.Error
	}

	var sys db.System
	if err = tx.Model(&db.System{}).FirstOrCreate(&sys).Error; err != nil {
		return err
	}
	if !sys.Running {
		return nil
	}

	if err = engine.Default().Stop(timeout); err != nil {
		return err
	}
	clearRunningNodeIndex()
	if err = tx.Model(&sys).Updates(map[string]interface{}{
		"running": false,
	}).Error; err != nil {
		return err
	}
	if err = tx.Commit().Error; err != nil {
		return err
	}
	committed = true
	return nil
}

func markStoppedAfterRestoreFailure(ctx context.Context, original error) error {
	tx := db.BeginTx(ctx)
	if tx.Error != nil {
		return fmt.Errorf("%w; %v", original, tx.Error)
	}
	var sys db.System
	if err := tx.Model(&sys).Select("id").First(&sys).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("%w; %v", original, err)
	}
	if err := tx.Model(&sys).Updates(map[string]interface{}{
		"running": false,
	}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("%w; %v", original, err)
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("%w; %v", original, err)
	}
	return original
}

func shouldRestoreRunningState(ctx context.Context) (ok bool, err error) {
	var sys db.System
	if err := db.DB(ctx).Model(&db.System{}).FirstOrCreate(&sys).Error; err != nil {
		return false, err
	}
	if !sys.Running {
		return false, nil
	}
	var m db.Config
	q := db.DB(ctx).Model(&db.Config{}).
		Where("selected = ?", true).
		First(&m)
	if q.Error != nil {
		return false, q.Error
	}
	if q.RowsAffected == 0 {
		logrus.Warnln("Data inconsistency detected: no selected config but last state is running")
		_ = db.DB(ctx).Model(&sys).Update("running", false).Error
		return false, nil
	}
	return true, nil
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
			wantedName := baseName
			for j := 0; ; j++ {
				_, exist := nameToNodes[wantedName]
				if !exist {
					nameToNodes[wantedName] = node
					break
				}
				wantedName = fmt.Sprintf("%v.%v", baseName, j)
			}
		}
	}
	for name, node := range nameToNodes {
		node.uniqueName = name
	}
}
