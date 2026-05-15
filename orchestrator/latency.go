/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"io"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	dialer "github.com/daeuniverse/dae/component/outbound/dialer"
	"github.com/daeuniverse/outbound/protocol/direct"
	"github.com/sirupsen/logrus"
)

const (
	latencyProbeConcurrency = 8
	nodeLatencyCacheTTL     = time.Hour
	nodeLatencyCacheMaxSize = 4096
)

type NodeLatencyResult struct {
	NodeID    uint
	LatencyMs *int32
	Alive     bool
	TestedAt  time.Time
	Message   *string
}

var nodeLatencyCache = struct {
	mu        sync.RWMutex
	updatedAt time.Time
	items     map[uint]*NodeLatencyResult
}{
	items: map[uint]*NodeLatencyResult{},
}

var runtimeNodeIndex = struct {
	mu        sync.RWMutex
	idsByName map[string]uint
}{
	idsByName: map[string]uint{},
}

func pruneNodeLatencyCacheLocked(now time.Time) {
	for id, result := range nodeLatencyCache.items {
		if result == nil || result.TestedAt.IsZero() || result.TestedAt.Add(nodeLatencyCacheTTL).Before(now) {
			delete(nodeLatencyCache.items, id)
		}
	}

	if len(nodeLatencyCache.items) <= nodeLatencyCacheMaxSize {
		return
	}

	type cacheEntry struct {
		id       uint
		testedAt time.Time
	}
	entries := make([]cacheEntry, 0, len(nodeLatencyCache.items))
	for id, result := range nodeLatencyCache.items {
		testedAt := time.Time{}
		if result != nil {
			testedAt = result.TestedAt
		}
		entries = append(entries, cacheEntry{id: id, testedAt: testedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].testedAt.Before(entries[j].testedAt)
	})
	for i := 0; i < len(entries)-nodeLatencyCacheMaxSize; i++ {
		delete(nodeLatencyCache.items, entries[i].id)
	}
}

func QueryNodeLatencies(ctx context.Context, ids []uint) ([]*NodeLatencyResult, error) {
	nodes, err := latencyProbeNodes(ctx, ids)
	if err != nil {
		return nil, err
	}

	merged := snapshotCachedNodeLatencyResults()
	runtimeResults, err := loadRuntimeNodeLatencyResults()
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
	pruneNodeLatencyCacheLocked(nodeLatencyCache.updatedAt)
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
	pruneNodeLatencyCacheLocked(nodeLatencyCache.updatedAt)
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
	prunedNeeded := false
	size := len(nodeLatencyCache.items)
	now := time.Now()
	for _, result := range nodeLatencyCache.items {
		if result == nil || result.TestedAt.IsZero() || result.TestedAt.Add(nodeLatencyCacheTTL).Before(now) {
			prunedNeeded = true
			break
		}
	}
	if !prunedNeeded && size <= nodeLatencyCacheMaxSize {
		results := make(map[uint]*NodeLatencyResult, len(nodeLatencyCache.items))
		for id, result := range nodeLatencyCache.items {
			results[id] = cloneNodeLatencyResult(result)
		}
		nodeLatencyCache.mu.RUnlock()
		return results
	}
	nodeLatencyCache.mu.RUnlock()

	nodeLatencyCache.mu.Lock()
	pruneNodeLatencyCacheLocked(now)
	results := make(map[uint]*NodeLatencyResult, len(nodeLatencyCache.items))
	for id, result := range nodeLatencyCache.items {
		results[id] = cloneNodeLatencyResult(result)
	}
	nodeLatencyCache.mu.Unlock()
	return results
}

func loadRuntimeNodeLatencyResults() (map[uint]*NodeLatencyResult, error) {
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

	results := make(map[uint]*NodeLatencyResult)
	for _, snapshot := range snapshots {
		if snapshot.CheckedAt.IsZero() && snapshot.LatencyMs == nil && snapshot.Message == "no latency result" {
			continue
		}

		nodeID, ok := runningNodeID(snapshot.Name)
		if !ok {
			continue
		}

		results[nodeID] = cloneNodeLatencyResult(&NodeLatencyResult{
			NodeID:    nodeID,
			LatencyMs: snapshot.LatencyMs,
			Alive:     snapshot.Alive,
			TestedAt:  snapshot.CheckedAt,
			Message:   stringPtr(snapshot.Message),
		})
	}

	storeNodeLatencyResults(mapsNodeLatencyValues(results))
	return results, nil
}

func replaceRunningNodeIndex(nodes []*node) {
	idsByName := make(map[string]uint, len(nodes))
	for _, node := range nodes {
		if node == nil || node.dbNode == nil || node.uniqueName == "" {
			continue
		}
		idsByName[node.uniqueName] = node.dbNode.ID
	}

	runtimeNodeIndex.mu.Lock()
	runtimeNodeIndex.idsByName = idsByName
	runtimeNodeIndex.mu.Unlock()
}

func clearRunningNodeIndex() {
	runtimeNodeIndex.mu.Lock()
	runtimeNodeIndex.idsByName = map[string]uint{}
	runtimeNodeIndex.mu.Unlock()
}

func runningNodeID(name string) (uint, bool) {
	runtimeNodeIndex.mu.RLock()
	defer runtimeNodeIndex.mu.RUnlock()
	id, ok := runtimeNodeIndex.idsByName[name]
	return id, ok
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
	option := dialer.NewGlobalOption(&parsedConfig.Global, log)
	resolverDNS, err := netip.ParseAddrPort(parsedConfig.Global.FallbackResolver)
	if err != nil {
		return nil, err
	}
	option.ResolverDialer = direct.NewDirectDialerLaddr(netip.Addr{}, direct.Option{
		FullCone:    false,
		FallbackDNS: parsedConfig.Global.FallbackResolver,
	})
	option.ResolverFullconeDialer = direct.NewDirectDialerLaddr(netip.Addr{}, direct.Option{
		FullCone:    true,
		FallbackDNS: parsedConfig.Global.FallbackResolver,
	})
	option.ResolverDNS = resolverDNS
	option.TcpCheckOptionRaw.ResolverDialer = option.ResolverDialer
	option.TcpCheckOptionRaw.ResolverDNS = resolverDNS
	option.CheckDnsOptionRaw.ResolverDialer = option.ResolverDialer
	option.CheckDnsOptionRaw.ResolverDNS = resolverDNS
	return option, nil
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

func NodeLatencyCacheStats() (entries int, updatedAt time.Time) {
	nodeLatencyCache.mu.Lock()
	defer nodeLatencyCache.mu.Unlock()
	pruneNodeLatencyCacheLocked(time.Now())
	return len(nodeLatencyCache.items), nodeLatencyCache.updatedAt
}

func stringPtr(value string) *string {
	return &value
}
