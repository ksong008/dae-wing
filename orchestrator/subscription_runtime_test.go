package orchestrator

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
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
	if err := db.UpsertNodeLatencyResults(context.Background(), []db.NodeLatencyResult{{
		NodeID:    node.ID,
		Alive:     true,
		TestedAt:  time.Now(),
		LatencyMs: int32Ptr(12),
	}}); err != nil {
		t.Fatalf("seed persisted latency: %v", err)
	}

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
	persisted, err := db.ListNodeLatencyResults(context.Background(), []uint{node.ID}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("list persisted latency: %v", err)
	}
	if len(persisted) != 0 {
		t.Fatalf("expected persisted latency to be deleted, got %d entries", len(persisted))
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
	if err := db.UpsertNodeLatencyResults(context.Background(), []db.NodeLatencyResult{{
		NodeID:    node.ID,
		Alive:     true,
		TestedAt:  time.Now(),
		LatencyMs: int32Ptr(23),
	}}); err != nil {
		t.Fatalf("seed persisted latency: %v", err)
	}
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
	persisted, err := db.ListNodeLatencyResults(context.Background(), []uint{node.ID}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("list persisted latency: %v", err)
	}
	if len(persisted) != 0 {
		t.Fatalf("expected persisted latency to be deleted after subscription delete, got %d entries", len(persisted))
	}
	if jobs, err = s.FindJobsByTag(subscriptionSchedulerTag(sub.ID)); err == nil && len(jobs) > 0 {
		t.Fatalf("expected scheduler job to be removed, still have %d", len(jobs))
	}
}

func TestRefreshSubscriptionUpdatesPreservedGroupNodeByName(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}

	oldLink := "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@old.example.com:443#node-a"
	newLink := "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@new.example.com:443#node-a"
	payload := base64.StdEncoding.EncodeToString([]byte(newLink + "\n"))
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte(payload))
	}))
	defer server.Close()

	sub := db.Subscription{
		UpdatedAt:  time.Now(),
		Link:       server.URL,
		CronExp:    "10 */6 * * *",
		CronEnable: false,
		Status:     "",
		Info:       "",
	}
	if err := db.DB(context.Background()).Create(&sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	oldNode, err := db.NewNodeModel(oldLink, nil, &sub.ID)
	if err != nil {
		t.Fatalf("build old node: %v", err)
	}
	if err := db.DB(context.Background()).Create(oldNode).Error; err != nil {
		t.Fatalf("seed old subscription node: %v", err)
	}

	group := db.Group{Name: "Proxy", Policy: "min"}
	if err := db.DB(context.Background()).Create(&group).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := db.DB(context.Background()).Model(&group).Association("Node").Append(oldNode); err != nil {
		t.Fatalf("associate group node: %v", err)
	}

	replaceNodeLatencyResults([]*NodeLatencyResult{{
		NodeID:   oldNode.ID,
		Alive:    true,
		TestedAt: time.Now(),
	}})
	if err := db.UpsertNodeLatencyResults(context.Background(), []db.NodeLatencyResult{{
		NodeID:    oldNode.ID,
		Alive:     true,
		TestedAt:  time.Now(),
		LatencyMs: int32Ptr(34),
	}}); err != nil {
		t.Fatalf("seed persisted latency: %v", err)
	}

	if _, err := RefreshSubscription(context.Background(), sub.ID); err != nil {
		t.Fatalf("refresh subscription: %v", err)
	}

	var nodes []db.Node
	if err := db.DB(context.Background()).Where("subscription_id = ?", sub.ID).Find(&nodes).Error; err != nil {
		t.Fatalf("list subscription nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("subscription node count = %d, want 1: %#v", len(nodes), nodes)
	}
	if nodes[0].ID != oldNode.ID {
		t.Fatalf("node ID = %d, want preserved ID %d", nodes[0].ID, oldNode.ID)
	}
	if nodes[0].Link != newLink {
		t.Fatalf("node link = %q, want %q", nodes[0].Link, newLink)
	}

	groupNodes, err := GetGroupNodes(context.Background(), group.ID)
	if err != nil {
		t.Fatalf("get group nodes: %v", err)
	}
	if len(groupNodes) != 1 || groupNodes[0].ID != oldNode.ID || groupNodes[0].Link != newLink {
		t.Fatalf("group nodes = %#v, want preserved node updated to new link", groupNodes)
	}
	if _, ok := snapshotCachedNodeLatencyResults()[oldNode.ID]; ok {
		t.Fatal("expected preserved node latency cache to be cleared after link update")
	}
	persisted, err := db.ListNodeLatencyResults(context.Background(), []uint{oldNode.ID}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("list persisted latency: %v", err)
	}
	if len(persisted) != 0 {
		t.Fatalf("expected persisted latency to be cleared after link update, got %d entries", len(persisted))
	}
}

func TestRefreshSubscriptionReloadsRunningRuntime(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	seedRunnableConfig(t)

	oldLink := "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@old.example.com:443#node-a"
	newLink := "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@new.example.com:443#node-a"
	payload := base64.StdEncoding.EncodeToString([]byte(newLink + "\n"))
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte(payload))
	}))
	defer server.Close()

	sub := db.Subscription{
		UpdatedAt:  time.Now(),
		Link:       server.URL,
		CronExp:    "10 */6 * * *",
		CronEnable: false,
		Status:     "",
		Info:       "",
	}
	if err := db.DB(context.Background()).Create(&sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	oldNode, err := db.NewNodeModel(oldLink, nil, &sub.ID)
	if err != nil {
		t.Fatalf("build old node: %v", err)
	}
	if err := db.DB(context.Background()).Create(oldNode).Error; err != nil {
		t.Fatalf("seed old subscription node: %v", err)
	}
	var group db.Group
	if err := db.DB(context.Background()).Where("name = ?", "proxy").First(&group).Error; err != nil {
		t.Fatalf("load proxy group: %v", err)
	}
	if err := db.DB(context.Background()).Model(&group).Association("Node").Append(oldNode); err != nil {
		t.Fatalf("associate group node: %v", err)
	}

	svc := newBlockingReloadService()
	svc.release()
	engine.SetDefault(svc)
	t.Cleanup(func() {
		engine.SetDefault(nil)
	})

	if _, err := Run(context.Background(), false); err != nil {
		t.Fatalf("initial run: %v", err)
	}
	if got := svc.reloadCalls.Load(); got != 1 {
		t.Fatalf("reload calls after initial run = %d, want 1", got)
	}

	if _, err := RefreshSubscription(context.Background(), sub.ID); err != nil {
		t.Fatalf("refresh subscription: %v", err)
	}
	if got := svc.reloadCalls.Load(); got != 2 {
		t.Fatalf("reload calls after refresh = %d, want 2", got)
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
