package orchestrator

import (
	"fmt"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
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

func TestReplaceRunningNodeIndex(t *testing.T) {
	clearRunningNodeIndex()
	defer clearRunningNodeIndex()

	replaceRunningNodeIndex([]*node{
		{
			dbNode:     &db.Node{ID: 11},
			uniqueName: "alpha",
		},
		{
			dbNode:     &db.Node{ID: 29},
			uniqueName: "beta",
		},
	})

	if id, ok := runningNodeID("alpha"); !ok || id != 11 {
		t.Fatalf("runningNodeID(alpha) = (%d, %v), want (11, true)", id, ok)
	}
	if id, ok := runningNodeID("beta"); !ok || id != 29 {
		t.Fatalf("runningNodeID(beta) = (%d, %v), want (29, true)", id, ok)
	}
	if _, ok := runningNodeID("missing"); ok {
		t.Fatal("expected missing runtime node key to be absent")
	}

	clearRunningNodeIndex()
	if _, ok := runningNodeID("alpha"); ok {
		t.Fatal("expected runtime node index to be cleared")
	}
}

func TestNodeLatencySyncIntervalClamp(t *testing.T) {
	tests := []struct {
		name          string
		checkInterval time.Duration
		want          time.Duration
	}{
		{name: "unset", checkInterval: 0, want: nodeLatencySyncDefaultInterval},
		{name: "below minimum", checkInterval: time.Second, want: nodeLatencySyncMinInterval},
		{name: "within range", checkInterval: 15 * time.Second, want: 15 * time.Second},
		{name: "above maximum", checkInterval: 10 * time.Minute, want: nodeLatencySyncMaxInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeLatencySyncInterval(tt.checkInterval); got != tt.want {
				t.Fatalf("nodeLatencySyncInterval(%v) = %v, want %v", tt.checkInterval, got, tt.want)
			}
		})
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
