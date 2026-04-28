/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	dialer "github.com/daeuniverse/dae/component/outbound/dialer"
	"github.com/sirupsen/logrus"
)

const latencyProbeConcurrency = 8

type NodeLatencyResult struct {
	NodeID    uint
	LatencyMs *int32
	Alive     bool
	TestedAt  time.Time
	Message   *string
}

var nodeLatencyCache = struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex
	updatedAt time.Time
	items     map[uint]*NodeLatencyResult
}{
	items: map[uint]*NodeLatencyResult{},
}

func QueryNodeLatencies(ctx context.Context, ids []uint) ([]*NodeLatencyResult, error) {
	_ = refreshNodeLatencyCacheIfNeeded(ctx)

	nodes, err := latencyProbeNodes(ctx, ids)
	if err != nil {
		return nil, err
	}

	merged := snapshotCachedNodeLatencyResults()
	runtimeResults, err := loadRuntimeNodeLatencyResults(ctx)
	if err != nil {
		return nil, err
	}
	for id, result := range runtimeResults {
		merged[id] = result
	}

	results := make([]*NodeLatencyResult, 0, len(nodes))
	for _, node := range nodes {
		if result, ok := merged[node.ID]; ok {
			results = append(results, cloneNodeLatencyResult(result))
		}
	}

	return results, nil
}

func TestNodeLatencies(ctx context.Context, ids []uint) ([]*NodeLatencyResult, error) {
	option, err := latencyProbeOption(ctx)
	if err != nil {
		return nil, err
	}

	nodes, err := latencyProbeNodes(ctx, ids)
	if err != nil {
		return nil, err
	}

	results := testNodeLatencyResultsForNodes(option, nodes)
	storeNodeLatencyResults(results)
	return results, nil
}

func cloneNodeLatencyResult(result *NodeLatencyResult) *NodeLatencyResult {
	if result == nil {
		return nil
	}

	clone := *result
	if result.LatencyMs != nil {
		latency := *result.LatencyMs
		clone.LatencyMs = &latency
	}
	if result.Message != nil {
		message := *result.Message
		clone.Message = &message
	}
	return &clone
}

func storeNodeLatencyResults(results []*NodeLatencyResult) {
	nodeLatencyCache.mu.Lock()
	defer nodeLatencyCache.mu.Unlock()

	nodeLatencyCache.updatedAt = time.Now()
	for _, result := range results {
		if result == nil {
			continue
		}
		nodeLatencyCache.items[result.NodeID] = cloneNodeLatencyResult(result)
	}
}

func replaceNodeLatencyResults(results []*NodeLatencyResult) {
	nodeLatencyCache.mu.Lock()
	defer nodeLatencyCache.mu.Unlock()

	nodeLatencyCache.updatedAt = time.Now()
	nodeLatencyCache.items = make(map[uint]*NodeLatencyResult, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		nodeLatencyCache.items[result.NodeID] = cloneNodeLatencyResult(result)
	}
}

func removeNodeLatencyResults(ids []uint) {
	if len(ids) == 0 {
		return
	}
	nodeLatencyCache.mu.Lock()
	defer nodeLatencyCache.mu.Unlock()
	for _, id := range ids {
		delete(nodeLatencyCache.items, id)
	}
}

func snapshotCachedNodeLatencyResults() map[uint]*NodeLatencyResult {
	nodeLatencyCache.mu.RLock()
	defer nodeLatencyCache.mu.RUnlock()

	results := make(map[uint]*NodeLatencyResult, len(nodeLatencyCache.items))
	for id, result := range nodeLatencyCache.items {
		results[id] = cloneNodeLatencyResult(result)
	}
	return results
}

func lastNodeLatencyCacheUpdatedAt() time.Time {
	nodeLatencyCache.mu.RLock()
	defer nodeLatencyCache.mu.RUnlock()
	return nodeLatencyCache.updatedAt
}

func loadRuntimeNodeLatencyResults(ctx context.Context) (map[uint]*NodeLatencyResult, error) {
	ctl, err := engine.Default().ControlPlane()
	if err != nil {
		if engine.Default().IsControlPlaneNotInit(err) {
			return map[uint]*NodeLatencyResult{}, nil
		}
		return nil, err
	}

	snapshots := ctl.SnapshotNodeLatencies()
	if len(snapshots) == 0 {
		return map[uint]*NodeLatencyResult{}, nil
	}

	links := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.Link == "" {
			continue
		}
		links = append(links, snapshot.Link)
	}
	if len(links) == 0 {
		return map[uint]*NodeLatencyResult{}, nil
	}

	var nodes []db.Node
	if err := db.DB(ctx).Where("link in ?", links).Find(&nodes).Error; err != nil {
		return nil, err
	}

	nodeByLink := make(map[string]db.Node, len(nodes))
	for _, node := range nodes {
		nodeByLink[node.Link] = node
	}

	results := make(map[uint]*NodeLatencyResult)
	for _, snapshot := range snapshots {
		if snapshot.CheckedAt.IsZero() && snapshot.LatencyMs == nil && snapshot.Message == "no latency result" {
			continue
		}

		node, ok := nodeByLink[snapshot.Link]
		if !ok {
			continue
		}

		results[node.ID] = cloneNodeLatencyResult(&NodeLatencyResult{
			NodeID:    node.ID,
			LatencyMs: snapshot.LatencyMs,
			Alive:     snapshot.Alive,
			TestedAt:  snapshot.CheckedAt,
			Message:   stringPtr(snapshot.Message),
		})
	}

	storeNodeLatencyResults(mapsNodeLatencyValues(results))
	return results, nil
}

func selectedCheckInterval(ctx context.Context) (time.Duration, error) {
	var configModel db.Config
	if err := db.DB(ctx).Where("selected = ?", true).First(&configModel).Error; err != nil {
		return 0, err
	}

	parsedConfig, err := engine.Default().ParseConfig(&configModel.Global, nil, nil)
	if err != nil {
		return 0, err
	}

	if parsedConfig.Global.CheckInterval <= 0 {
		return 30 * time.Second, nil
	}

	return parsedConfig.Global.CheckInterval, nil
}

func refreshNodeLatencyCache(ctx context.Context) error {
	option, err := latencyProbeOption(ctx)
	if err != nil {
		return err
	}

	nodes, err := latencyProbeNodes(ctx, nil)
	if err != nil {
		return err
	}

	results := testNodeLatencyResultsForNodes(option, nodes)
	replaceNodeLatencyResults(results)

	if ctl, err := engine.Default().ControlPlane(); err == nil {
		ctl.TriggerLatencyChecks()
	}

	return nil
}

func refreshNodeLatencyCacheIfNeeded(ctx context.Context) error {
	interval, err := selectedCheckInterval(ctx)
	if err != nil {
		return err
	}

	lastUpdated := lastNodeLatencyCacheUpdatedAt()
	if !lastUpdated.IsZero() && time.Since(lastUpdated) < interval {
		return nil
	}

	nodeLatencyCache.refreshMu.Lock()
	defer nodeLatencyCache.refreshMu.Unlock()

	lastUpdated = lastNodeLatencyCacheUpdatedAt()
	if !lastUpdated.IsZero() && time.Since(lastUpdated) < interval {
		return nil
	}

	return refreshNodeLatencyCache(ctx)
}

func latencyProbeOption(ctx context.Context) (*dialer.GlobalOption, error) {
	var configModel db.Config
	if err := db.DB(ctx).Where("selected = ?", true).First(&configModel).Error; err != nil {
		return nil, err
	}

	parsedConfig, err := engine.Default().ParseConfig(&configModel.Global, nil, nil)
	if err != nil {
		return nil, err
	}

	log := logrus.New()
	log.SetOutput(io.Discard)
	return dialer.NewGlobalOption(&parsedConfig.Global, log), nil
}

func latencyProbeNodes(ctx context.Context, ids []uint) ([]db.Node, error) {
	q := db.DB(ctx).Model(&db.Node{})
	if ids != nil {
		q = q.Where("id in ?", ids)
	}

	var nodes []db.Node
	if err := q.Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func testNodeLatencyResultsForNodes(option *dialer.GlobalOption, nodes []db.Node) []*NodeLatencyResult {
	results := make([]*NodeLatencyResult, len(nodes))
	sem := make(chan struct{}, latencyProbeConcurrency)
	var wg sync.WaitGroup

	for index := range nodes {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			node := nodes[index]
			results[index] = testSingleNodeLatency(option, &node)
		}()
	}

	wg.Wait()
	return results
}

func testSingleNodeLatency(option *dialer.GlobalOption, node *db.Node) *NodeLatencyResult {
	result := &NodeLatencyResult{
		NodeID:   node.ID,
		Alive:    false,
		TestedAt: time.Now(),
	}

	d, err := dialer.NewFromLink(option, dialer.InstanceOption{DisableCheck: false}, node.Link, "")
	if err != nil {
		result.Message = stringPtr(err.Error())
		return result
	}
	defer d.Close()

	probeResult, err := d.ProbeLatency()
	if err != nil {
		result.Message = stringPtr(err.Error())
		return result
	}

	result.Alive = probeResult.Alive
	result.TestedAt = probeResult.CheckedAt
	if probeResult.Alive {
		latencyMs := int32(probeResult.Latency.Milliseconds())
		result.LatencyMs = &latencyMs
		return result
	}
	if probeResult.Message != "" {
		result.Message = stringPtr(probeResult.Message)
	}
	return result
}

func mapsNodeLatencyValues(items map[uint]*NodeLatencyResult) []*NodeLatencyResult {
	results := make([]*NodeLatencyResult, 0, len(items))
	for _, item := range items {
		results = append(results, item)
	}
	return results
}

func stringPtr(value string) *string {
	return &value
}
