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

func resetNodeLatencyCacheForQueryTest() {
	nodeLatencyCache.mu.Lock()
	defer nodeLatencyCache.mu.Unlock()

	nodeLatencyCache.updatedAt = time.Time{}
	nodeLatencyCache.items = map[uint]*NodeLatencyResult{}
}
