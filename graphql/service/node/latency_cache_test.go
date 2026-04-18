/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package node

import (
	"testing"
	"time"
)

func resetNodeLatencyCache() {
	nodeLatencyCache.mu.Lock()
	defer nodeLatencyCache.mu.Unlock()
	nodeLatencyCache.updatedAt = time.Time{}
	nodeLatencyCache.items = map[uint]*LatencyResolver{}
}

func TestStoreLatencyResultsReplacesOldEntries(t *testing.T) {
	resetNodeLatencyCache()

	oldLatency := int32(10)
	nodeLatencyCache.items[1] = &LatencyResolver{NodeID: 1, LatencyMsV: &oldLatency}
	nodeLatencyCache.items[2] = &LatencyResolver{NodeID: 2, LatencyMsV: &oldLatency}

	newLatency := int32(20)
	storeLatencyResults([]*LatencyResolver{
		{NodeID: 2, LatencyMsV: &newLatency},
		{NodeID: 3, LatencyMsV: &newLatency},
	})

	nodeLatencyCache.mu.RLock()
	defer nodeLatencyCache.mu.RUnlock()
	if len(nodeLatencyCache.items) != 2 {
		t.Fatalf("expected cache to contain exactly 2 entries, got %d", len(nodeLatencyCache.items))
	}
	if _, ok := nodeLatencyCache.items[1]; ok {
		t.Fatal("expected old node entry to be removed on cache replacement")
	}
	if _, ok := nodeLatencyCache.items[2]; !ok {
		t.Fatal("expected refreshed node entry to remain")
	}
	if _, ok := nodeLatencyCache.items[3]; !ok {
		t.Fatal("expected new node entry to exist")
	}
}

func TestSnapshotCachedLatencyResultsForOnlyRequestedNodes(t *testing.T) {
	resetNodeLatencyCache()

	latency1 := int32(11)
	latency2 := int32(22)
	nodeLatencyCache.items[1] = &LatencyResolver{NodeID: 1, LatencyMsV: &latency1}
	nodeLatencyCache.items[2] = &LatencyResolver{NodeID: 2, LatencyMsV: &latency2}

	results := snapshotCachedLatencyResultsFor([]uint{2})
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if _, ok := results[1]; ok {
		t.Fatal("did not expect unrelated node in filtered snapshot")
	}
	resolver, ok := results[2]
	if !ok {
		t.Fatal("expected requested node to be returned")
	}
	if resolver.LatencyMsV == nil || *resolver.LatencyMsV != latency2 {
		t.Fatalf("unexpected latency value: %#v", resolver.LatencyMsV)
	}

	// Verify returned entries are clones and do not mutate cache in place.
	updated := int32(99)
	resolver.LatencyMsV = &updated
	if *nodeLatencyCache.items[2].LatencyMsV != latency2 {
		t.Fatal("expected snapshot clone to be detached from cache entry")
	}
}
