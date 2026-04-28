package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
)

func TestDeleteNodesClearsLatencyCache(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}

	node := db.Node{Link: "ss://cache-node", Name: "Cache Node", Address: "127.0.0.1", Protocol: "ss"}
	if err := db.DB(context.Background()).Create(&node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	replaceNodeLatencyResults([]*NodeLatencyResult{{
		NodeID:   node.ID,
		Alive:    true,
		TestedAt: time.Now(),
	}})

	if _, ok := snapshotCachedNodeLatencyResults()[node.ID]; !ok {
		t.Fatal("expected latency cache entry before delete")
	}

	removed, err := DeleteNodes(context.Background(), []uint{node.ID})
	if err != nil {
		t.Fatalf("delete nodes: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := snapshotCachedNodeLatencyResults()[node.ID]; ok {
		t.Fatal("expected latency cache entry to be cleared")
	}
}

func TestDeleteSubscriptionsClearsLatencyCacheAndScheduler(t *testing.T) {
	resetSubscriptionSchedulerForTest()
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}

	sub := db.Subscription{
		UpdatedAt:  time.Now(),
		Link:       "https://example.invalid/sub",
		CronExp:    "10 */6 * * *",
		CronEnable: true,
		Status:     "",
		Info:       "",
	}
	if err := db.DB(context.Background()).Create(&sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	node := db.Node{Link: "ss://sub-node", Name: "Sub Node", Address: "127.0.0.1", Protocol: "ss", SubscriptionID: &sub.ID}
	if err := db.DB(context.Background()).Create(&node).Error; err != nil {
		t.Fatalf("seed subscription node: %v", err)
	}

	replaceNodeLatencyResults([]*NodeLatencyResult{{
		NodeID:   node.ID,
		Alive:    true,
		TestedAt: time.Now(),
	}})
	AddSubscriptionUpdateScheduler(context.Background(), sub.ID)

	subscriptionSchedulerMu.Lock()
	s := ensureSubscriptionSchedulerLocked()
	subscriptionSchedulerMu.Unlock()
	jobs, err := s.FindJobsByTag(subscriptionSchedulerTag(sub.ID))
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected one scheduler job, jobs=%d err=%v", len(jobs), err)
	}

	removed, err := DeleteSubscriptions(context.Background(), []uint{sub.ID})
	if err != nil {
		t.Fatalf("delete subscriptions: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := snapshotCachedNodeLatencyResults()[node.ID]; ok {
		t.Fatal("expected latency cache entry to be cleared after subscription delete")
	}
	if jobs, err = s.FindJobsByTag(subscriptionSchedulerTag(sub.ID)); err == nil && len(jobs) > 0 {
		t.Fatalf("expected scheduler job to be removed, still have %d", len(jobs))
	}
}

func resetSubscriptionSchedulerForTest() {
	subscriptionSchedulerMu.Lock()
	defer subscriptionSchedulerMu.Unlock()
	if subscriptionScheduler != nil {
		subscriptionScheduler.Clear()
		subscriptionScheduler.Stop()
		subscriptionScheduler = nil
	}
}
