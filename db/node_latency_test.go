package db

import (
	"context"
	"testing"
	"time"
)

func TestNodeLatencyResultUpsertKeepsNewestResult(t *testing.T) {
	if err := InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("InitDatabase() error = %v", err)
	}

	node := Node{Link: "ss://latency-node", Name: "Latency Node", Address: "127.0.0.1", Protocol: "ss"}
	if err := DB(context.Background()).Create(&node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	oldAt := time.Now().Add(-time.Minute)
	newAt := time.Now()
	if err := UpsertNodeLatencyResults(context.Background(), []NodeLatencyResult{{
		NodeID:    node.ID,
		LatencyMs: int32Ptr(200),
		Alive:     true,
		TestedAt:  oldAt,
	}}); err != nil {
		t.Fatalf("upsert old result: %v", err)
	}
	if err := UpsertNodeLatencyResults(context.Background(), []NodeLatencyResult{{
		NodeID:    node.ID,
		LatencyMs: int32Ptr(40),
		Alive:     true,
		TestedAt:  newAt,
	}}); err != nil {
		t.Fatalf("upsert new result: %v", err)
	}
	if err := UpsertNodeLatencyResults(context.Background(), []NodeLatencyResult{{
		NodeID:    node.ID,
		LatencyMs: int32Ptr(900),
		Alive:     true,
		TestedAt:  oldAt,
	}}); err != nil {
		t.Fatalf("upsert stale result: %v", err)
	}

	results, err := ListNodeLatencyResults(context.Background(), []uint{node.ID}, oldAt.Add(-time.Second))
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].LatencyMs == nil || *results[0].LatencyMs != 40 {
		t.Fatalf("latency = %v, want 40", results[0].LatencyMs)
	}

	results, err = ListNodeLatencyResults(context.Background(), []uint{node.ID}, newAt.Add(time.Second))
	if err != nil {
		t.Fatalf("list filtered results: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("filtered results len = %d, want 0", len(results))
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
