package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/orchestrator"
)

func TestAuthAndUserHandlers(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	handler := NewHandler()

	health := performRawRequest(handler, http.MethodGet, "/health", "", nil)
	if health.Code != http.StatusOK {
		t.Fatalf("health code = %d, body = %s", health.Code, health.Body.String())
	}

	status := performRawRequest(handler, http.MethodGet, "/auth/status", "", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("auth status code = %d, body = %s", status.Code, status.Body.String())
	}

	create := performRawRequest(handler, http.MethodPost, "/auth/users", `{"username":"admin","password":"abc123"}`, nil)
	if create.Code != http.StatusCreated {
		t.Fatalf("create user code = %d, body = %s", create.Code, create.Body.String())
	}
	created := decodeBody(t, create)
	if created["token"].(string) == "" {
		t.Fatalf("create user token is empty")
	}

	token := performRawRequest(handler, http.MethodPost, "/auth/token", `{"username":"admin","password":"abc123"}`, nil)
	if token.Code != http.StatusOK {
		t.Fatalf("token code = %d, body = %s", token.Code, token.Body.String())
	}

	var user db.User
	if err := db.DB(context.Background()).Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}

	me := performRawRequest(handler, http.MethodGet, "/user/me", "", &user)
	if me.Code != http.StatusOK {
		t.Fatalf("me code = %d, body = %s", me.Code, me.Body.String())
	}

	patch := performRawRequest(handler, http.MethodPatch, "/user/me", `{"name":"Admin","clearAvatar":true}`, &user)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch user code = %d, body = %s", patch.Code, patch.Body.String())
	}

	setStorage := performRawRequest(handler, http.MethodPut, "/user/me/storage", `{"paths":["ui.sidebar"],"values":["open"]}`, &user)
	if setStorage.Code != http.StatusOK {
		t.Fatalf("set storage code = %d, body = %s", setStorage.Code, setStorage.Body.String())
	}

	getStorage := performRawRequest(handler, http.MethodGet, "/user/me/storage?path=ui.sidebar", "", &user)
	if getStorage.Code != http.StatusOK {
		t.Fatalf("get storage code = %d, body = %s", getStorage.Code, getStorage.Body.String())
	}

	ensureDefaultsBody := `{"configName":"global","global":{"logLevel":"info"},"dnsName":"default","dns":"upstream {\n  googledns: 'udp://1.1.1.1:53'\n}\nrouting {\n  request {\n    fallback: googledns\n  }\n}","routingName":"default","routing":"fallback: proxy","groupName":"proxy","policy":"random","policyParams":[],"mode":"rule"}`
	ensureDefaults := performRawRequest(handler, http.MethodPost, "/user/me/default-resources", ensureDefaultsBody, &user)
	if ensureDefaults.Code != http.StatusOK {
		t.Fatalf("ensure default resources code = %d, body = %s", ensureDefaults.Code, ensureDefaults.Body.String())
	}
	defaults := decodeBody(t, ensureDefaults)
	if defaults["defaultConfigID"] == "" || defaults["defaultRoutingID"] == "" || defaults["defaultDNSID"] == "" || defaults["defaultGroupID"] == "" {
		t.Fatalf("ensure default resources response = %#v", defaults)
	}
	if got := defaults["mode"].(string); got != "rule" {
		t.Fatalf("ensure default resources mode = %q, want rule", got)
	}

	changePassword := performRawRequest(handler, http.MethodPost, "/user/me/password", `{"currentPassword":"abc123","newPassword":"def456"}`, &user)
	if changePassword.Code != http.StatusOK {
		t.Fatalf("change password code = %d, body = %s", changePassword.Code, changePassword.Body.String())
	}
}

func TestGeneralAndGroupHandlers(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	if err := db.DB(context.Background()).Create(&db.Config{Name: "cfg", Global: "global {}", Selected: true}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	handler := NewHandler()

	if token, err := orchestrator.CreateUser(context.Background(), "admin", "abc123"); err != nil || token == "" {
		t.Fatalf("seed auth user: %v", err)
	}
	var user db.User
	if err := db.DB(context.Background()).Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("load auth user: %v", err)
	}

	state := performRawRequest(handler, http.MethodGet, "/general/state", "", &user)
	if state.Code != http.StatusOK {
		t.Fatalf("general state code = %d, body = %s", state.Code, state.Body.String())
	}

	runtimeOverview := performRawRequest(handler, http.MethodGet, "/runtime/overview", "", &user)
	if runtimeOverview.Code != http.StatusOK {
		t.Fatalf("runtime overview code = %d, body = %s", runtimeOverview.Code, runtimeOverview.Body.String())
	}
	runtimeBody := decodeBody(t, runtimeOverview)
	if _, ok := runtimeBody["rssBytes"].(string); !ok {
		t.Fatalf("runtime rssBytes = %#v", runtimeBody["rssBytes"])
	}
	if _, ok := runtimeBody["heapAllocBytes"].(string); !ok {
		t.Fatalf("runtime heapAllocBytes = %#v", runtimeBody["heapAllocBytes"])
	}
	if _, ok := runtimeBody["goroutines"].(float64); !ok {
		t.Fatalf("runtime goroutines = %#v", runtimeBody["goroutines"])
	}

	interfaces := performRawRequest(handler, http.MethodGet, "/general/interfaces", "", &user)
	if interfaces.Code != http.StatusOK && !strings.Contains(interfaces.Body.String(), "operation not permitted") {
		t.Fatalf("general interfaces code = %d, body = %s", interfaces.Code, interfaces.Body.String())
	}

	cacheStats := performRawRequest(handler, http.MethodGet, "/general/cache-stats", "", &user)
	if cacheStats.Code != http.StatusOK {
		t.Fatalf("cache stats code = %d, body = %s", cacheStats.Code, cacheStats.Body.String())
	}
	cacheStatsBody := decodeBody(t, cacheStats)
	if _, ok := cacheStatsBody["dnsCacheEntries"].(float64); !ok {
		t.Fatalf("dnsCacheEntries = %#v", cacheStatsBody["dnsCacheEntries"])
	}
	if _, ok := cacheStatsBody["nodeLatencyCacheEntries"].(float64); !ok {
		t.Fatalf("nodeLatencyCacheEntries = %#v", cacheStatsBody["nodeLatencyCacheEntries"])
	}

	subTag := "sub"
	sub := db.Subscription{Link: "https://example.invalid/sub", Tag: &subTag, CronExp: "10 */6 * * *", CronEnable: true}
	if err := db.DB(context.Background()).Create(&sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	node := db.Node{Link: "ss://seed", Name: "Seed", Address: "127.0.0.1", Protocol: "ss"}
	if err := db.DB(context.Background()).Create(&node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	createGroup := performRawRequest(handler, http.MethodPost, "/groups", `{"name":"proxy","policy":"random","policyParams":[]}`, &user)
	if createGroup.Code != http.StatusCreated {
		t.Fatalf("create group code = %d, body = %s", createGroup.Code, createGroup.Body.String())
	}
	group := decodeBody(t, createGroup)
	groupID := int(group["id"].(float64))

	addNodes := performRawRequest(handler, http.MethodPost, "/groups/"+itoa(groupID)+"/nodes", `{"nodeIds":[`+itoa(int(node.ID))+`]}`, &user)
	if addNodes.Code != http.StatusOK {
		t.Fatalf("add group nodes code = %d, body = %s", addNodes.Code, addNodes.Body.String())
	}

	addSubscriptions := performRawRequest(handler, http.MethodPost, "/groups/"+itoa(groupID)+"/subscriptions", `{"subscriptionIds":[`+itoa(int(sub.ID))+`]}`, &user)
	if addSubscriptions.Code != http.StatusOK {
		t.Fatalf("add group subscriptions code = %d, body = %s", addSubscriptions.Code, addSubscriptions.Body.String())
	}

	getGroup := performRawRequest(handler, http.MethodGet, "/groups/"+itoa(groupID), "", &user)
	if getGroup.Code != http.StatusOK {
		t.Fatalf("get group code = %d, body = %s", getGroup.Code, getGroup.Body.String())
	}
	groupBody := decodeBody(t, getGroup)
	subscriptions, ok := groupBody["subscriptions"].([]any)
	if !ok || len(subscriptions) != 1 {
		t.Fatalf("group subscriptions = %#v", groupBody["subscriptions"])
	}
	binding, ok := subscriptions[0].(map[string]any)
	if !ok {
		t.Fatalf("group subscription binding = %#v", subscriptions[0])
	}
	if _, ok := binding["updatedAt"].(string); !ok {
		t.Fatalf("group subscription updatedAt = %#v", binding["updatedAt"])
	}
	if _, ok := binding["status"].(string); !ok {
		t.Fatalf("group subscription status = %#v", binding["status"])
	}
	if _, ok := binding["info"].(string); !ok {
		t.Fatalf("group subscription info = %#v", binding["info"])
	}

	updateGroup := performRawRequest(handler, http.MethodPut, "/groups/"+itoa(groupID), `{"policy":"fixed","policyParams":[]}`, &user)
	if updateGroup.Code != http.StatusOK {
		t.Fatalf("update group code = %d, body = %s", updateGroup.Code, updateGroup.Body.String())
	}
}

func TestUserDAEBundleHandlers(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	handler := NewHandler()

	if token, err := orchestrator.CreateUser(context.Background(), "admin", "abc123"); err != nil || token == "" {
		t.Fatalf("seed auth user: %v", err)
	}
	var user db.User
	if err := db.DB(context.Background()).Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("load auth user: %v", err)
	}

	cfgActive := db.Config{Name: "cfg-active", Global: "global {\n  log_level: info\n}", Selected: true}
	cfgDefault := db.Config{Name: "cfg-default", Global: "global {\n  log_level: warn\n}", Selected: false}
	if err := db.DB(context.Background()).Create(&cfgActive).Error; err != nil {
		t.Fatalf("seed active config: %v", err)
	}
	if err := db.DB(context.Background()).Create(&cfgDefault).Error; err != nil {
		t.Fatalf("seed default config: %v", err)
	}

	dnsActive := db.Dns{Name: "dns-active", Dns: "dns {\n  upstream {\n    googledns: 'udp://8.8.8.8:53'\n  }\n}", Selected: true}
	dnsDefault := db.Dns{Name: "dns-default", Dns: "dns {\n  upstream {\n    alidns: 'udp://223.5.5.5:53'\n  }\n}", Selected: false}
	if err := db.DB(context.Background()).Create(&dnsActive).Error; err != nil {
		t.Fatalf("seed active dns: %v", err)
	}
	if err := db.DB(context.Background()).Create(&dnsDefault).Error; err != nil {
		t.Fatalf("seed default dns: %v", err)
	}

	routingActive := db.Routing{Name: "routing-active", Routing: "routing {\n  fallback: proxy\n}", Selected: true}
	routingDefault := db.Routing{Name: "routing-default", Routing: "routing {\n  fallback: direct\n}", Selected: false}
	if err := db.DB(context.Background()).Create(&routingActive).Error; err != nil {
		t.Fatalf("seed active routing: %v", err)
	}
	if err := db.DB(context.Background()).Create(&routingDefault).Error; err != nil {
		t.Fatalf("seed default routing: %v", err)
	}

	subTag := "sub-backup"
	subscription := db.Subscription{
		UpdatedAt:  time.Unix(1_717_171_717, 0).UTC(),
		Link:       "https://example.invalid/subscription",
		CronExp:    "10 */6 * * *",
		CronEnable: true,
		Status:     "ok",
		Info:       "provider",
		Tag:        &subTag,
	}
	if err := db.DB(context.Background()).Create(&subscription).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	manualTag := "manual-node"
	manualNode := db.Node{Link: "ss://manual", Name: "Manual", Address: "127.0.0.1", Protocol: "ss", Tag: &manualTag}
	subNode := db.Node{Link: "ss://subscription", Name: "Subscription", Address: "127.0.0.2", Protocol: "ss", SubscriptionID: &subscription.ID}
	if err := db.DB(context.Background()).Create(&manualNode).Error; err != nil {
		t.Fatalf("seed manual node: %v", err)
	}
	if err := db.DB(context.Background()).Create(&subNode).Error; err != nil {
		t.Fatalf("seed subscription node: %v", err)
	}

	regex := "^HK"
	group := db.Group{
		Name:   "proxy",
		Policy: "fixed",
		PolicyParams: []db.GroupPolicyParam{{
			Key:   "index",
			Value: "0",
		}},
	}
	if err := db.DB(context.Background()).Create(&group).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := db.DB(context.Background()).Model(&group).Association("Node").Append(&manualNode); err != nil {
		t.Fatalf("bind group node: %v", err)
	}
	if err := db.DB(context.Background()).Create(&db.GroupSubscription{
		GroupID:         group.ID,
		SubscriptionID:  subscription.ID,
		NameFilterRegex: &regex,
	}).Error; err != nil {
		t.Fatalf("bind group subscription: %v", err)
	}

	tx := db.BeginTx(context.Background())
	if err := setJSONStorageWithTx(tx, &user, []string{"defaultConfigID", "defaultRoutingID", "defaultDNSID", "defaultGroupID", "mode"}, []string{
		itoa(int(cfgDefault.ID)),
		itoa(int(routingDefault.ID)),
		itoa(int(dnsDefault.ID)),
		itoa(int(group.ID)),
		"rule",
	}); err != nil {
		t.Fatalf("seed defaults storage: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit defaults storage: %v", err)
	}

	exportResp := performRawRequest(handler, http.MethodGet, "/user/me/dae-bundle", "", &user)
	if exportResp.Code != http.StatusOK {
		t.Fatalf("export bundle code = %d, body = %s", exportResp.Code, exportResp.Body.String())
	}

	var bundle daeBundle
	if err := json.Unmarshal(exportResp.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if bundle.SchemaVersion != daeBundleSchemaVersion {
		t.Fatalf("bundle schema version = %d, want %d", bundle.SchemaVersion, daeBundleSchemaVersion)
	}
	if len(bundle.Configs) != 2 || len(bundle.DNSS) != 2 || len(bundle.Routings) != 2 {
		t.Fatalf("bundle resource counts = configs:%d dnss:%d routings:%d", len(bundle.Configs), len(bundle.DNSS), len(bundle.Routings))
	}
	if len(bundle.Subscriptions) != 1 || len(bundle.Nodes) != 2 || len(bundle.Groups) != 1 {
		t.Fatalf("bundle graph counts = subscriptions:%d nodes:%d groups:%d", len(bundle.Subscriptions), len(bundle.Nodes), len(bundle.Groups))
	}
	if bundle.Selected.ConfigID == nil || *bundle.Selected.ConfigID != cfgActive.ID {
		t.Fatalf("bundle selected config = %#v, want %d", bundle.Selected.ConfigID, cfgActive.ID)
	}
	if bundle.Defaults.ConfigID == nil || *bundle.Defaults.ConfigID != cfgDefault.ID {
		t.Fatalf("bundle default config = %#v, want %d", bundle.Defaults.ConfigID, cfgDefault.ID)
	}

	if err := db.DB(context.Background()).Create(&db.Config{Name: "extra-config", Global: "global {}", Selected: false}).Error; err != nil {
		t.Fatalf("seed extra config: %v", err)
	}

	importResp := performRawRequest(handler, http.MethodPut, "/user/me/dae-bundle", exportResp.Body.String(), &user)
	if importResp.Code != http.StatusOK {
		t.Fatalf("import bundle code = %d, body = %s", importResp.Code, importResp.Body.String())
	}

	var configs []db.Config
	if err := db.DB(context.Background()).Order("id asc").Find(&configs).Error; err != nil {
		t.Fatalf("reload configs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("config count after import = %d, want 2", len(configs))
	}
	var selectedConfig db.Config
	if err := db.DB(context.Background()).Where("selected = ?", true).First(&selectedConfig).Error; err != nil {
		t.Fatalf("selected config after import: %v", err)
	}
	if selectedConfig.Name != "cfg-active" {
		t.Fatalf("selected config after import = %q, want cfg-active", selectedConfig.Name)
	}

	if err := db.DB(context.Background()).Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("reload user after import: %v", err)
	}
	defaults := orchestrator.QueryJSONStorage(&user, []string{"defaultConfigID", "defaultRoutingID", "defaultDNSID", "defaultGroupID", "mode"})
	if len(defaults) != 5 || defaults[4] != "rule" {
		t.Fatalf("defaults after import = %#v", defaults)
	}
	defaultConfigID, ok := parseStoredUint(defaults[0])
	if !ok {
		t.Fatalf("default config id not stored: %#v", defaults[0])
	}
	var defaultConfig db.Config
	if err := db.DB(context.Background()).First(&defaultConfig, defaultConfigID).Error; err != nil {
		t.Fatalf("load default config after import: %v", err)
	}
	if defaultConfig.Name != "cfg-default" {
		t.Fatalf("default config after import = %q, want cfg-default", defaultConfig.Name)
	}

	groups, err := orchestrator.ListGroups(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("list groups after import: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("group count after import = %d, want 1", len(groups))
	}
	importedGroup := groups[0]
	if importedGroup.Name != "proxy" {
		t.Fatalf("imported group name = %q, want proxy", importedGroup.Name)
	}
	if len(importedGroup.Node) != 1 || importedGroup.Node[0].Tag == nil || *importedGroup.Node[0].Tag != manualTag {
		t.Fatalf("imported group nodes = %#v", importedGroup.Node)
	}
	if len(importedGroup.SubscriptionBindings) != 1 {
		t.Fatalf("imported group bindings = %#v", importedGroup.SubscriptionBindings)
	}
	if importedGroup.SubscriptionBindings[0].NameFilterRegex == nil || *importedGroup.SubscriptionBindings[0].NameFilterRegex != regex {
		t.Fatalf("imported group regex = %#v", importedGroup.SubscriptionBindings[0].NameFilterRegex)
	}
}

func TestUserDAEConfigFileHandlers(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	handler := NewHandler()

	if token, err := orchestrator.CreateUser(context.Background(), "admin", "abc123"); err != nil || token == "" {
		t.Fatalf("seed auth user: %v", err)
	}
	var user db.User
	if err := db.DB(context.Background()).Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("load auth user: %v", err)
	}

	subServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte("c3M6Ly8yMDIyLWJsYWtlMy1hZXMtMTI4LWdjbTpNVEl6TkRVMk56ZzVNREV5TXpRMU5nPT1AZXhhbXBsZS5jb206NDQzI3N1Yi1ub2RlCg=="))
	}))
	defer subServer.Close()
	subServer2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		payload := base64.StdEncoding.EncodeToString([]byte("ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@unused.example.com:443#unused-sub-node\n"))
		_, _ = rw.Write([]byte(payload))
	}))
	defer subServer2.Close()

	cfg := db.Config{Name: "active", Global: "global {\n  log_level: info\n}", Selected: true}
	dns := db.Dns{Name: "active_dns", Dns: "dns {\n  upstream {\n    googledns: 'udp://1.1.1.1:53'\n  }\n  routing {\n    request {\n      fallback: googledns\n    }\n  }\n}", Selected: true}
	routing := db.Routing{Name: "active_routing", Routing: "routing {\n  fallback: proxy\n}", Selected: true}
	if err := db.DB(context.Background()).Create(&cfg).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.DB(context.Background()).Create(&dns).Error; err != nil {
		t.Fatalf("seed dns: %v", err)
	}
	if err := db.DB(context.Background()).Create(&routing).Error; err != nil {
		t.Fatalf("seed routing: %v", err)
	}

	subTag := "suba"
	subscription := db.Subscription{
		UpdatedAt:  time.Now(),
		Link:       subServer.URL,
		CronExp:    "10 */6 * * *",
		CronEnable: true,
		Status:     "ok",
		Info:       "seed",
		Tag:        &subTag,
	}
	if err := db.DB(context.Background()).Create(&subscription).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	unusedSubTag := "subb"
	unusedSubscription := db.Subscription{
		UpdatedAt:  time.Now(),
		Link:       subServer2.URL,
		CronExp:    "10 */6 * * *",
		CronEnable: true,
		Status:     "ok",
		Info:       "unused",
		Tag:        &unusedSubTag,
	}
	if err := db.DB(context.Background()).Create(&unusedSubscription).Error; err != nil {
		t.Fatalf("seed unused subscription: %v", err)
	}
	subNode := db.Node{
		Link:           "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@example.com:443#sub-node",
		Name:           "sub-node",
		Address:        "example.com:443",
		Protocol:       "shadowsocks",
		SubscriptionID: &subscription.ID,
	}
	manualTag := "manual-node"
	manualNode := db.Node{
		Link:     "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@manual.example:443#manual",
		Name:     "manual",
		Address:  "manual.example:443",
		Protocol: "shadowsocks",
		Tag:      &manualTag,
	}
	if err := db.DB(context.Background()).Create(&subNode).Error; err != nil {
		t.Fatalf("seed subscription node: %v", err)
	}
	unusedSubNode := db.Node{
		Link:           "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@unused.example.com:443#unused-sub-node",
		Name:           "unused-sub-node",
		Address:        "unused.example.com:443",
		Protocol:       "shadowsocks",
		SubscriptionID: &unusedSubscription.ID,
	}
	if err := db.DB(context.Background()).Create(&unusedSubNode).Error; err != nil {
		t.Fatalf("seed unused subscription node: %v", err)
	}
	if err := db.DB(context.Background()).Create(&manualNode).Error; err != nil {
		t.Fatalf("seed manual node: %v", err)
	}
	spareTag := "spare-node"
	spareNode := db.Node{
		Link:     "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng==@spare.example:443#spare",
		Name:     "spare",
		Address:  "spare.example:443",
		Protocol: "shadowsocks",
		Tag:      &spareTag,
	}
	if err := db.DB(context.Background()).Create(&spareNode).Error; err != nil {
		t.Fatalf("seed spare manual node: %v", err)
	}

	regex := "^sub-"
	group := db.Group{
		Name:   "proxy",
		Policy: "fixed",
		PolicyParams: []db.GroupPolicyParam{{
			Key:   "",
			Value: "0",
		}},
	}
	if err := db.DB(context.Background()).Create(&group).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := db.DB(context.Background()).Model(&group).Association("Node").Append(&manualNode); err != nil {
		t.Fatalf("bind manual node: %v", err)
	}
	if err := db.DB(context.Background()).Create(&db.GroupSubscription{
		GroupID:         group.ID,
		SubscriptionID:  subscription.ID,
		NameFilterRegex: &regex,
	}).Error; err != nil {
		t.Fatalf("bind subscription group: %v", err)
	}
	subscriptionNodeGroup := db.Group{
		Name:   "subnode",
		Policy: "fixed",
	}
	if err := db.DB(context.Background()).Create(&subscriptionNodeGroup).Error; err != nil {
		t.Fatalf("seed subscription-backed manual-node group: %v", err)
	}
	if err := db.DB(context.Background()).Model(&subscriptionNodeGroup).Association("Node").Append(&subNode); err != nil {
		t.Fatalf("bind subscription-backed manual node: %v", err)
	}

	exportResp := performRawRequest(handler, http.MethodGet, "/user/me/dae-config-file", "", &user)
	if exportResp.Code != http.StatusOK {
		t.Fatalf("export dae config code = %d, body = %s", exportResp.Code, exportResp.Body.String())
	}
	exported := decodeBody(t, exportResp)
	if warnings, ok := exported["warnings"].([]any); ok && len(warnings) > 0 {
		t.Fatalf("unexpected export warnings = %#v", warnings)
	}
	content, ok := exported["content"].(string)
	if !ok || content == "" {
		t.Fatalf("exported content = %#v", exported["content"])
	}
	if !strings.Contains(content, "subscription {") || !strings.Contains(content, "node {") || !strings.Contains(content, "group {") {
		t.Fatalf("exported content missing expected sections:\n%s", content)
	}
	if !strings.Contains(content, `subtag("suba")`) {
		t.Fatalf("exported content missing subtag binding:\n%s", content)
	}
	if !strings.Contains(content, `name("manual-node")`) {
		t.Fatalf("exported content missing manual node filter:\n%s", content)
	}
	if !strings.Contains(content, subServer2.URL) {
		t.Fatalf("exported content missing unused subscription:\n%s", content)
	}
	if !strings.Contains(content, spareTag) {
		t.Fatalf("exported content missing spare node tag:\n%s", content)
	}

	if err := db.DB(context.Background()).Create(&db.Config{Name: "extra", Global: "global {}", Selected: false}).Error; err != nil {
		t.Fatalf("seed extra config: %v", err)
	}

	importBody := fmt.Sprintf(`{"filename":"selected.dae","namePrefix":"restored","content":%q}`, content)
	previewResp := performRawRequest(handler, http.MethodPost, "/user/me/dae-config-file/preview", importBody, &user)
	if previewResp.Code != http.StatusOK {
		t.Fatalf("preview dae config code = %d, body = %s", previewResp.Code, previewResp.Body.String())
	}
	previewBody := decodeBody(t, previewResp)
	previewBundle, ok := previewBody["bundle"].(map[string]any)
	if !ok {
		t.Fatalf("preview bundle = %#v", previewBody["bundle"])
	}
	if got := previewBundle["mode"].(string); got != "rule" {
		t.Fatalf("preview bundle mode = %q, want rule", got)
	}
	previewConfigs := previewBundle["configs"].([]any)
	if len(previewConfigs) != 1 {
		t.Fatalf("preview configs len = %d, want 1", len(previewConfigs))
	}
	firstPreviewConfig := previewConfigs[0].(map[string]any)
	if got := firstPreviewConfig["name"].(string); got != "restored" {
		t.Fatalf("preview config name = %q, want restored", got)
	}

	importResp := performRawRequest(handler, http.MethodPut, "/user/me/dae-config-file", importBody, &user)
	if importResp.Code != http.StatusOK {
		t.Fatalf("import dae config code = %d, body = %s", importResp.Code, importResp.Body.String())
	}

	var configs []db.Config
	if err := db.DB(context.Background()).Find(&configs).Error; err != nil {
		t.Fatalf("load configs after import: %v", err)
	}
	if len(configs) != 1 || configs[0].Name != "restored" || !configs[0].Selected {
		t.Fatalf("configs after import = %#v", configs)
	}

	var dnss []db.Dns
	if err := db.DB(context.Background()).Find(&dnss).Error; err != nil {
		t.Fatalf("load dns after import: %v", err)
	}
	if len(dnss) != 1 || dnss[0].Name != "restored_dns" || !dnss[0].Selected {
		t.Fatalf("dns after import = %#v", dnss)
	}

	var routings []db.Routing
	if err := db.DB(context.Background()).Find(&routings).Error; err != nil {
		t.Fatalf("load routing after import: %v", err)
	}
	if len(routings) != 1 || routings[0].Name != "restored_routing" || !routings[0].Selected {
		t.Fatalf("routing after import = %#v", routings)
	}

	var subscriptions []db.Subscription
	if err := db.DB(context.Background()).Find(&subscriptions).Error; err != nil {
		t.Fatalf("load subscriptions after import: %v", err)
	}
	subscriptionTags := make([]string, 0, len(subscriptions))
	for _, item := range subscriptions {
		if item.Tag != nil {
			subscriptionTags = append(subscriptionTags, *item.Tag)
		}
	}
	slices.Sort(subscriptionTags)
	if len(subscriptions) != 2 || !slices.Equal(subscriptionTags, []string{"suba", "subb"}) {
		t.Fatalf("subscriptions after import = %#v", subscriptions)
	}

	var nodes []db.Node
	if err := db.DB(context.Background()).Find(&nodes).Error; err != nil {
		t.Fatalf("load nodes after import: %v", err)
	}
	nodeTags := make([]string, 0, len(nodes))
	for _, item := range nodes {
		if item.Tag != nil {
			nodeTags = append(nodeTags, *item.Tag)
		}
	}
	slices.Sort(nodeTags)
	if len(nodes) != 4 || !slices.Equal(nodeTags, []string{"manual-node", "spare-node"}) {
		t.Fatalf("nodes after import = %#v", nodes)
	}

	groups, err := orchestrator.ListGroups(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("list groups after import: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups after import = %#v", groups)
	}
	groupByName := make(map[string]db.Group, len(groups))
	for _, item := range groups {
		groupByName[item.Name] = item
	}
	importedGroup, ok := groupByName["proxy"]
	if !ok {
		t.Fatalf("proxy group missing after import: %#v", groups)
	}
	if importedGroup.Name != "proxy" || importedGroup.Policy != "fixed" {
		t.Fatalf("imported group = %#v", importedGroup)
	}
	if len(importedGroup.Node) != 1 || importedGroup.Node[0].Tag == nil || *importedGroup.Node[0].Tag != manualTag {
		t.Fatalf("imported group nodes = %#v", importedGroup.Node)
	}
	if len(importedGroup.SubscriptionBindings) != 1 || importedGroup.SubscriptionBindings[0].NameFilterRegex == nil || *importedGroup.SubscriptionBindings[0].NameFilterRegex != regex {
		t.Fatalf("imported group subscription bindings = %#v", importedGroup.SubscriptionBindings)
	}
	subnodeGroup, ok := groupByName["subnode"]
	if !ok {
		t.Fatalf("subnode group missing after import: %#v", groups)
	}
	if len(subnodeGroup.SubscriptionBindings) != 0 {
		t.Fatalf("subnode group bindings = %#v, want none", subnodeGroup.SubscriptionBindings)
	}
	if len(subnodeGroup.Node) != 1 || subnodeGroup.Node[0].Name != "sub-node" || subnodeGroup.Node[0].SubscriptionID == nil || *subnodeGroup.Node[0].SubscriptionID == 0 {
		t.Fatalf("subnode group nodes = %#v", subnodeGroup.Node)
	}
}

func performRawRequest(handler http.Handler, method string, path string, body string, user *db.User) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
