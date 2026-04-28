package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	schema := performRawRequest(handler, http.MethodGet, "/general/schema", "", &user)
	if schema.Code != http.StatusOK {
		t.Fatalf("general schema code = %d, body = %s", schema.Code, schema.Body.String())
	}

	interfaces := performRawRequest(handler, http.MethodGet, "/general/interfaces", "", &user)
	if interfaces.Code != http.StatusOK && !strings.Contains(interfaces.Body.String(), "operation not permitted") {
		t.Fatalf("general interfaces code = %d, body = %s", interfaces.Code, interfaces.Body.String())
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

	getGroupByName := performRawRequest(handler, http.MethodGet, "/groups/by-name/proxy", "", &user)
	if getGroupByName.Code != http.StatusOK {
		t.Fatalf("get group by name code = %d, body = %s", getGroupByName.Code, getGroupByName.Body.String())
	}

	updateGroup := performRawRequest(handler, http.MethodPut, "/groups/"+itoa(groupID), `{"policy":"fixed","policyParams":[]}`, &user)
	if updateGroup.Code != http.StatusOK {
		t.Fatalf("update group code = %d, body = %s", updateGroup.Code, updateGroup.Body.String())
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
