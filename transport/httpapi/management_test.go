package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
)

func TestNodeManagementHandlers(t *testing.T) {
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
	handler := NewHandler()

	tag := "node-a"
	model := db.Node{
		Link:     "ss://seed-node",
		Name:     "Seed Node",
		Address:  "127.0.0.1",
		Protocol: "ss",
		Tag:      &tag,
	}
	if err := db.DB(context.Background()).Create(&model).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	model2 := db.Node{
		Link:     "ss://seed-node-2",
		Name:     "Seed Node 2",
		Address:  "127.0.0.2",
		Protocol: "ss",
	}
	if err := db.DB(context.Background()).Create(&model2).Error; err != nil {
		t.Fatalf("seed node2: %v", err)
	}
	nodeID := int(model.ID)
	nodeID2 := int(model2.ID)

	list := performJSONRequest(t, handler, http.MethodGet, "/nodes?independent=true", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list nodes status = %d, body = %s", list.Code, list.Body.String())
	}

	defaultList := performJSONRequest(t, handler, http.MethodGet, "/nodes", "")
	if defaultList.Code != http.StatusOK {
		t.Fatalf("default list nodes status = %d, body = %s", defaultList.Code, defaultList.Body.String())
	}
	defaultListBody := decodeBody(t, defaultList)
	if got := int(defaultListBody["totalCount"].(float64)); got != 2 {
		t.Fatalf("default independent totalCount = %d, want 2", got)
	}

	paged := performJSONRequest(t, handler, http.MethodGet, "/nodes?independent=true&limit=1", "")
	if paged.Code != http.StatusOK {
		t.Fatalf("paged nodes status = %d, body = %s", paged.Code, paged.Body.String())
	}
	pagedBody := decodeBody(t, paged)
	nextAfterID, ok := pagedBody["nextAfterId"].(float64)
	if !ok || nextAfterID == 0 {
		t.Fatalf("node nextAfterId = %#v, want non-zero", pagedBody["nextAfterId"])
	}
	nextPage := performJSONRequest(t, handler, http.MethodGet, "/nodes?independent=true&afterId="+itoa(int(nextAfterID))+"&limit=1", "")
	if nextPage.Code != http.StatusOK {
		t.Fatalf("next page nodes status = %d, body = %s", nextPage.Code, nextPage.Body.String())
	}

	get := performJSONRequest(t, handler, http.MethodGet, "/nodes/"+itoa(nodeID), "")
	if get.Code != http.StatusOK {
		t.Fatalf("get node status = %d, body = %s", get.Code, get.Body.String())
	}

	latencies := performJSONRequest(t, handler, http.MethodGet, "/nodes/latencies?ids="+itoa(nodeID), "")
	if latencies.Code != http.StatusOK {
		t.Fatalf("latency query status = %d, body = %s", latencies.Code, latencies.Body.String())
	}

	httpNode := db.Node{
		Link:     "http://user:pass@127.0.0.1:8080#http-node",
		Name:     "HTTP Node",
		Address:  "127.0.0.1:8080",
		Protocol: "http",
	}
	if err := db.DB(context.Background()).Create(&httpNode).Error; err != nil {
		t.Fatalf("seed http node: %v", err)
	}
	httpNodeResp := performJSONRequest(t, handler, http.MethodGet, "/nodes/"+itoa(int(httpNode.ID)), "")
	if httpNodeResp.Code != http.StatusOK {
		t.Fatalf("get http node status = %d, body = %s", httpNodeResp.Code, httpNodeResp.Body.String())
	}
	httpNodeBody := decodeBody(t, httpNodeResp)
	if got, ok := httpNodeBody["transport"].(string); !ok || got != "http" {
		t.Fatalf("http node transport = %#v, want http", httpNodeBody["transport"])
	}

	ss2022Node := db.Node{
		Link:     "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@example.com:443#ss2022-node",
		Name:     "SS2022 Node",
		Address:  "example.com:443",
		Protocol: "shadowsocks",
	}
	if err := db.DB(context.Background()).Create(&ss2022Node).Error; err != nil {
		t.Fatalf("seed ss2022 node: %v", err)
	}
	ss2022NodeResp := performJSONRequest(t, handler, http.MethodGet, "/nodes/"+itoa(int(ss2022Node.ID)), "")
	if ss2022NodeResp.Code != http.StatusOK {
		t.Fatalf("get ss2022 node status = %d, body = %s", ss2022NodeResp.Code, ss2022NodeResp.Body.String())
	}
	ss2022NodeBody := decodeBody(t, ss2022NodeResp)
	if got, ok := ss2022NodeBody["transport"].(string); !ok || got != "ss2022" {
		t.Fatalf("ss2022 node transport = %#v, want ss2022", ss2022NodeBody["transport"])
	}

	vlessVisionNode := db.Node{
		Link:     "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&security=reality&sni=example.com&fp=chrome&pbk=abc&sid=123&type=tcp&flow=xtls-rprx-vision#vision",
		Name:     "VLESS Vision Node",
		Address:  "example.com:443",
		Protocol: "vless",
	}
	if err := db.DB(context.Background()).Create(&vlessVisionNode).Error; err != nil {
		t.Fatalf("seed vless vision node: %v", err)
	}
	vlessVisionNodeResp := performJSONRequest(t, handler, http.MethodGet, "/nodes/"+itoa(int(vlessVisionNode.ID)), "")
	if vlessVisionNodeResp.Code != http.StatusOK {
		t.Fatalf("get vless vision node status = %d, body = %s", vlessVisionNodeResp.Code, vlessVisionNodeResp.Body.String())
	}
	vlessVisionNodeBody := decodeBody(t, vlessVisionNodeResp)
	if got, ok := vlessVisionNodeBody["transport"].(string); !ok || got != "vision" {
		t.Fatalf("vless vision node transport = %#v, want vision", vlessVisionNodeBody["transport"])
	}

	remove := performJSONRequest(t, handler, http.MethodDelete, "/nodes/"+itoa(nodeID), "")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete node status = %d, body = %s", remove.Code, remove.Body.String())
	}

	batchRemove := performJSONRequest(t, handler, http.MethodDelete, "/nodes", `{"ids":[`+itoa(nodeID2)+`]}`)
	if batchRemove.Code != http.StatusOK {
		t.Fatalf("batch delete node status = %d, body = %s", batchRemove.Code, batchRemove.Body.String())
	}
	batchRemoved := decodeBody(t, batchRemove)
	if got := int(batchRemoved["removed"].(float64)); got != 1 {
		t.Fatalf("batch removed nodes = %d, want 1", got)
	}
}

func TestSubscriptionManagementHandlers(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	handler := NewHandler()
	tag := "sub-a"
	sub := db.Subscription{
		UpdatedAt:  time.Now(),
		Link:       "https://example.invalid/sub",
		CronExp:    "10 */6 * * *",
		CronEnable: true,
		Status:     "",
		Info:       "",
		Tag:        &tag,
	}
	if err := db.DB(context.Background()).Create(&sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	for i := 0; i < 2; i++ {
		node := db.Node{
			Link:           fmt.Sprintf("ss://subscription-node-%d", i),
			Name:           fmt.Sprintf("Subscription Node %d", i),
			Address:        "127.0.0.1",
			Protocol:       "ss",
			SubscriptionID: &sub.ID,
		}
		if err := db.DB(context.Background()).Create(&node).Error; err != nil {
			t.Fatalf("seed node: %v", err)
		}
	}
	subscriptionID := int(sub.ID)

	list := performJSONRequest(t, handler, http.MethodGet, "/subscriptions", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list subscriptions status = %d, body = %s", list.Code, list.Body.String())
	}

	listBody := decodeBody(t, list)
	items := listBody["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("subscription items len = %d, want 1", len(items))
	}
	if got := int(items[0].(map[string]any)["nodeCount"].(float64)); got != 2 {
		t.Fatalf("subscription nodeCount = %d, want 2", got)
	}

	expandedList := performJSONRequest(t, handler, http.MethodGet, "/subscriptions?expand=nodes", "")
	if expandedList.Code != http.StatusOK {
		t.Fatalf("expanded list subscriptions status = %d, body = %s", expandedList.Code, expandedList.Body.String())
	}
	expandedListBody := decodeBody(t, expandedList)
	expandedItems := expandedListBody["items"].([]any)
	nodes, ok := expandedItems[0].(map[string]any)["nodes"].(map[string]any)
	if !ok {
		t.Fatalf("subscription nodes payload = %#v", expandedItems[0].(map[string]any)["nodes"])
	}
	if got := int(nodes["totalCount"].(float64)); got != 2 {
		t.Fatalf("expanded subscription nodes totalCount = %d, want 2", got)
	}

	get := performJSONRequest(t, handler, http.MethodGet, "/subscriptions/"+itoa(subscriptionID), "")
	if get.Code != http.StatusOK {
		t.Fatalf("get subscription status = %d, body = %s", get.Code, get.Body.String())
	}

	subNodes := performJSONRequest(t, handler, http.MethodGet, "/subscriptions/"+itoa(subscriptionID)+"/nodes?limit=1", "")
	if subNodes.Code != http.StatusOK {
		t.Fatalf("subscription nodes status = %d, body = %s", subNodes.Code, subNodes.Body.String())
	}
	subNodesBody := decodeBody(t, subNodes)
	if got := int(subNodesBody["totalCount"].(float64)); got != 2 {
		t.Fatalf("subscription nodes totalCount = %d, want 2", got)
	}
	subNextAfterID, ok := subNodesBody["nextAfterId"].(float64)
	if !ok || subNextAfterID == 0 {
		t.Fatalf("subscription nodes nextAfterId = %#v, want non-zero", subNodesBody["nextAfterId"])
	}

	update := performJSONRequest(t, handler, http.MethodPut, "/subscriptions/"+itoa(subscriptionID), `{"cronExp":"10 */12 * * *","cronEnable":false}`)
	if update.Code != http.StatusOK {
		t.Fatalf("update subscription status = %d, body = %s", update.Code, update.Body.String())
	}
	updated := decodeBody(t, update)
	if got := updated["cronEnable"].(bool); got {
		t.Fatalf("cronEnable = true, want false")
	}

	remove := performJSONRequest(t, handler, http.MethodDelete, "/subscriptions/"+itoa(subscriptionID), "")
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete subscription status = %d, body = %s", remove.Code, remove.Body.String())
	}

	sub2 := db.Subscription{
		UpdatedAt:  time.Now(),
		Link:       "https://example.invalid/sub-2",
		CronExp:    "10 */6 * * *",
		CronEnable: true,
	}
	if err := db.DB(context.Background()).Create(&sub2).Error; err != nil {
		t.Fatalf("seed subscription2: %v", err)
	}
	batchRemove := performJSONRequest(t, handler, http.MethodDelete, "/subscriptions", `{"ids":[`+itoa(int(sub2.ID))+`]}`)
	if batchRemove.Code != http.StatusOK {
		t.Fatalf("batch delete subscription status = %d, body = %s", batchRemove.Code, batchRemove.Body.String())
	}
	batchRemoved := decodeBody(t, batchRemove)
	if got := int(batchRemoved["removed"].(float64)); got != 1 {
		t.Fatalf("batch removed subscriptions = %d, want 1", got)
	}
}

func itoa(value int) string {
	return fmt.Sprintf("%d", value)
}
