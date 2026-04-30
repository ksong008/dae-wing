package orchestrator

import (
	"fmt"
	"testing"
	"time"
)

func TestStoreNodeLatencyResultsPrunesExpiredEntries(t *testing.T) {
	replaceNodeLatencyResults([]*NodeLatencyResult{{
		NodeID:    1,
		Alive:     true,
		TestedAt:  time.Now().Add(-nodeLatencyCacheTTL - time.Minute),
		LatencyMs: int32Ptr(10),
	}})

	storeNodeLatencyResults([]*NodeLatencyResult{{
		NodeID:    2,
		Alive:     true,
		TestedAt:  time.Now(),
		LatencyMs: int32Ptr(20),
	}})

	results := snapshotCachedNodeLatencyResults()
	if _, ok := results[1]; ok {
		t.Fatal("expected expired latency cache entry to be pruned")
	}
	if _, ok := results[2]; !ok {
		t.Fatal("expected fresh latency cache entry to remain")
	}
}

func TestStoreNodeLatencyResultsCapsCacheSize(t *testing.T) {
	results := make([]*NodeLatencyResult, 0, nodeLatencyCacheMaxSize+1)
	now := time.Now()
	for i := 0; i < nodeLatencyCacheMaxSize; i++ {
		results = append(results, &NodeLatencyResult{
			NodeID:    uint(i + 1),
			Alive:     true,
			TestedAt:  now.Add(time.Duration(i) * time.Second),
			LatencyMs: int32Ptr(int32(i)),
			Message:   stringPtr(fmt.Sprintf("node-%d", i+1)),
		})
	}
	replaceNodeLatencyResults(results)

	storeNodeLatencyResults([]*NodeLatencyResult{{
		NodeID:    uint(nodeLatencyCacheMaxSize + 1),
		Alive:     true,
		TestedAt:  now.Add(2 * time.Hour),
		LatencyMs: int32Ptr(999),
	}})

	snapshot := snapshotCachedNodeLatencyResults()
	if len(snapshot) != nodeLatencyCacheMaxSize {
		t.Fatalf("expected capped latency cache size %d, got %d", nodeLatencyCacheMaxSize, len(snapshot))
	}
	if _, ok := snapshot[1]; ok {
		t.Fatal("expected oldest latency cache entry to be evicted")
	}
	if _, ok := snapshot[uint(nodeLatencyCacheMaxSize+1)]; !ok {
		t.Fatal("expected newest latency cache entry to remain")
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
