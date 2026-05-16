package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
)

func TestQueryNodeLatenciesDoesNotRefreshAllNodes(t *testing.T) {
	resetNodeLatencyCacheForQueryTest()
	t.Cleanup(resetNodeLatencyCacheForQueryTest)

	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	if err := db.DB(context.Background()).Create(&db.Config{
		Name:     "cfg",
		Global:   "global {}",
		Selected: true,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}

	requested := db.Node{Link: "ss://requested-node", Name: "Requested", Address: "127.0.0.1", Protocol: "ss"}
	unrequested := db.Node{Link: "ss://unrequested-node", Name: "Unrequested", Address: "127.0.0.2", Protocol: "ss"}
	if err := db.DB(context.Background()).Create(&requested).Error; err != nil {
		t.Fatalf("seed requested node: %v", err)
	}
	if err := db.DB(context.Background()).Create(&unrequested).Error; err != nil {
		t.Fatalf("seed unrequested node: %v", err)
	}

	if results, err := QueryNodeLatencies(context.Background(), []uint{requested.ID}); err != nil {
		t.Fatalf("query node latencies: %v", err)
	} else if len(results) != 0 {
		t.Fatalf("passive query returned %d latency results, want 0", len(results))
	}

	cache := snapshotCachedNodeLatencyResults()
	if _, ok := cache[requested.ID]; ok {
		t.Fatal("passive query should not cache requested node")
	}
	if _, ok := cache[unrequested.ID]; ok {
		t.Fatal("passive query should not refresh and cache an unrequested node")
	}
}

func TestQueryNodeLatenciesReturnsPersistedResult(t *testing.T) {
	resetNodeLatencyCacheForQueryTest()
	t.Cleanup(resetNodeLatencyCacheForQueryTest)

	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	node := db.Node{Link: "ss://persisted-node", Name: "Persisted", Address: "127.0.0.1", Protocol: "ss"}
	if err := db.DB(context.Background()).Create(&node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	testedAt := time.Now()
	if err := db.UpsertNodeLatencyResults(context.Background(), []db.NodeLatencyResult{{
		NodeID:    node.ID,
		LatencyMs: int32Ptr(33),
		Alive:     true,
		TestedAt:  testedAt,
	}}); err != nil {
		t.Fatalf("seed persisted latency: %v", err)
	}

	results, err := QueryNodeLatencies(context.Background(), []uint{node.ID})
	if err != nil {
		t.Fatalf("query node latencies: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].LatencyMs == nil || *results[0].LatencyMs != 33 {
		t.Fatalf("latency = %v, want 33", results[0].LatencyMs)
	}
	if !results[0].TestedAt.Equal(testedAt) {
		t.Fatalf("testedAt = %v, want %v", results[0].TestedAt, testedAt)
	}
}

func TestQueryNodeLatenciesReturnsPersistedResultOlderThanOneHour(t *testing.T) {
	resetNodeLatencyCacheForQueryTest()
	t.Cleanup(resetNodeLatencyCacheForQueryTest)

	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	node := db.Node{Link: "ss://persisted-node", Name: "Persisted", Address: "127.0.0.1", Protocol: "ss"}
	if err := db.DB(context.Background()).Create(&node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	testedAt := time.Now().Add(-2 * time.Hour)
	if err := db.UpsertNodeLatencyResults(context.Background(), []db.NodeLatencyResult{{
		NodeID:    node.ID,
		LatencyMs: int32Ptr(41),
		Alive:     true,
		TestedAt:  testedAt,
	}}); err != nil {
		t.Fatalf("seed persisted latency: %v", err)
	}

	results, err := QueryNodeLatencies(context.Background(), []uint{node.ID})
	if err != nil {
		t.Fatalf("query node latencies: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].LatencyMs == nil || *results[0].LatencyMs != 41 {
		t.Fatalf("latency = %v, want 41", results[0].LatencyMs)
	}
}

func resetNodeLatencyCacheForQueryTest() {
	nodeLatencyCache.mu.Lock()
	defer nodeLatencyCache.mu.Unlock()

	stopNodeLatencySyncWorker()
	nodeLatencyCache.updatedAt = time.Time{}
	nodeLatencyCache.items = map[uint]*NodeLatencyResult{}
}
