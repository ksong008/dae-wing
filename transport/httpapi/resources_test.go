package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
)

func TestResourceCRUDHandlers(t *testing.T) {
	tests := []struct {
		name        string
		collection  string
		valueField  string
		createBody  string
		updateBody  string
		renameBody  string
		initialName string
		updatedName string
	}{
		{
			name:        "config",
			collection:  "/configs",
			valueField:  "global",
			createBody:  `{"name":"cfg-1","global":"global {}"}`,
			updateBody:  `{"name":"cfg-2","global":"global {}"}`,
			renameBody:  `{"name":"cfg-3"}`,
			initialName: "cfg-1",
			updatedName: "cfg-2",
		},
		{
			name:        "dns",
			collection:  "/dns",
			valueField:  "dns",
			createBody:  `{"name":"dns-1","dns":"dns {}"}`,
			updateBody:  `{"name":"dns-2","dns":"dns {}"}`,
			renameBody:  `{"name":"dns-3"}`,
			initialName: "dns-1",
			updatedName: "dns-2",
		},
		{
			name:        "routing",
			collection:  "/routings",
			valueField:  "routing",
			createBody:  `{"name":"routing-1","routing":"routing {}"}`,
			updateBody:  `{"name":"routing-2","routing":"routing {}"}`,
			renameBody:  `{"name":"routing-3"}`,
			initialName: "routing-1",
			updatedName: "routing-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := db.InitDatabase(t.TempDir()); err != nil {
				t.Fatalf("init database: %v", err)
			}
			handler := NewHandler()

			create := performJSONRequest(t, handler, http.MethodPost, tt.collection, tt.createBody)
			if create.Code != http.StatusCreated {
				t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
			}
			created := decodeBody(t, create)
			id := int(created["id"].(float64))
			if got := created["name"].(string); got != tt.initialName {
				t.Fatalf("created name = %q, want %q", got, tt.initialName)
			}

			get := performJSONRequest(t, handler, http.MethodGet, fmt.Sprintf("%s/%d", tt.collection, id), "")
			if get.Code != http.StatusOK {
				t.Fatalf("get status = %d, body = %s", get.Code, get.Body.String())
			}
			fetched := decodeBody(t, get)
			if got := fetched[tt.valueField].(string); got == "" {
				t.Fatalf("fetched %s is empty", tt.valueField)
			}
			switch tt.name {
			case "config":
				if _, ok := fetched["parsedGlobal"].(map[string]any); !ok {
					t.Fatalf("parsedGlobal = %#v", fetched["parsedGlobal"])
				}
			case "dns":
				if _, ok := fetched["parsedDns"].(map[string]any); !ok {
					t.Fatalf("parsedDns = %#v", fetched["parsedDns"])
				}
			case "routing":
				if _, ok := fetched["parsedRouting"].(map[string]any); !ok {
					t.Fatalf("parsedRouting = %#v", fetched["parsedRouting"])
				}
			}

			listByID := performJSONRequest(t, handler, http.MethodGet, fmt.Sprintf("%s?id=%d", tt.collection, id), "")
			if listByID.Code != http.StatusOK {
				t.Fatalf("list by id status = %d, body = %s", listByID.Code, listByID.Body.String())
			}
			var filtered struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(listByID.Body.Bytes(), &filtered); err != nil {
				t.Fatalf("decode filtered list: %v", err)
			}
			if len(filtered.Items) != 1 {
				t.Fatalf("filtered items len = %d, want 1", len(filtered.Items))
			}
			switch tt.name {
			case "config":
				if _, ok := filtered.Items[0]["parsedGlobal"]; ok {
					t.Fatalf("parsedGlobal should be omitted on collection reads without expand")
				}
				listExpanded := performJSONRequest(t, handler, http.MethodGet, fmt.Sprintf("%s?id=%d&expand=parsed", tt.collection, id), "")
				if listExpanded.Code != http.StatusOK {
					t.Fatalf("expanded list status = %d, body = %s", listExpanded.Code, listExpanded.Body.String())
				}
				var expanded struct {
					Items []map[string]any `json:"items"`
				}
				if err := json.Unmarshal(listExpanded.Body.Bytes(), &expanded); err != nil {
					t.Fatalf("decode expanded list: %v", err)
				}
				if _, ok := expanded.Items[0]["parsedGlobal"].(map[string]any); !ok {
					t.Fatalf("expanded parsedGlobal = %#v", expanded.Items[0]["parsedGlobal"])
				}
			case "dns":
				if _, ok := filtered.Items[0]["parsedDns"]; ok {
					t.Fatalf("parsedDns should be omitted on collection reads without expand")
				}
			case "routing":
				if _, ok := filtered.Items[0]["parsedRouting"]; ok {
					t.Fatalf("parsedRouting should be omitted on collection reads without expand")
				}
			}

			update := performJSONRequest(t, handler, http.MethodPut, fmt.Sprintf("%s/%d", tt.collection, id), tt.updateBody)
			if update.Code != http.StatusOK {
				t.Fatalf("update status = %d, body = %s", update.Code, update.Body.String())
			}
			updated := decodeBody(t, update)
			if got := updated["name"].(string); got != tt.updatedName {
				t.Fatalf("updated name = %q, want %q", got, tt.updatedName)
			}
			if got := int(updated["version"].(float64)); got != 1 {
				t.Fatalf("updated version = %d, want 1", got)
			}

			rename := performJSONRequest(t, handler, http.MethodPut, fmt.Sprintf("%s/%d", tt.collection, id), tt.renameBody)
			if rename.Code != http.StatusOK {
				t.Fatalf("rename status = %d, body = %s", rename.Code, rename.Body.String())
			}
			renamed := decodeBody(t, rename)
			if got := int(renamed["version"].(float64)); got != 1 {
				t.Fatalf("renamed version = %d, want 1", got)
			}

			selectResp := performJSONRequest(t, handler, http.MethodPost, fmt.Sprintf("%s/%d/select", tt.collection, id), "")
			if selectResp.Code != http.StatusOK {
				t.Fatalf("select status = %d, body = %s", selectResp.Code, selectResp.Body.String())
			}

			listSelected := performJSONRequest(t, handler, http.MethodGet, tt.collection+"?selected=true", "")
			if listSelected.Code != http.StatusOK {
				t.Fatalf("selected list status = %d, body = %s", listSelected.Code, listSelected.Body.String())
			}
			var selected struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(listSelected.Body.Bytes(), &selected); err != nil {
				t.Fatalf("decode selected list: %v", err)
			}
			if len(selected.Items) != 1 {
				t.Fatalf("selected items len = %d, want 1", len(selected.Items))
			}
			if got := int(selected.Items[0]["id"].(float64)); got != id {
				t.Fatalf("selected id = %d, want %d", got, id)
			}

			remove := performJSONRequest(t, handler, http.MethodDelete, fmt.Sprintf("%s/%d", tt.collection, id), "")
			if remove.Code != http.StatusNoContent {
				t.Fatalf("delete status = %d, body = %s", remove.Code, remove.Body.String())
			}

			missing := performJSONRequest(t, handler, http.MethodGet, fmt.Sprintf("%s/%d", tt.collection, id), "")
			if missing.Code != http.StatusNotFound {
				t.Fatalf("missing status = %d, body = %s", missing.Code, missing.Body.String())
			}
		})
	}
}

func TestResourceCreateAndResetUseEngineEmptySections(t *testing.T) {
	tests := []struct {
		name         string
		collection   string
		valueField   string
		createBody   string
		resetBody    string
		emptySection string
	}{
		{
			name:         "config",
			collection:   "/configs",
			valueField:   "global",
			createBody:   `{"name":"cfg-default"}`,
			resetBody:    `{"global":"   "}`,
			emptySection: engine.Default().EmptyGlobalSection(),
		},
		{
			name:         "dns",
			collection:   "/dns",
			valueField:   "dns",
			createBody:   `{"name":"dns-default"}`,
			resetBody:    `{"dns":"   "}`,
			emptySection: engine.Default().EmptyDnsSection(),
		},
		{
			name:         "routing",
			collection:   "/routings",
			valueField:   "routing",
			createBody:   `{"name":"routing-default"}`,
			resetBody:    `{"routing":"   "}`,
			emptySection: engine.Default().EmptyRoutingSection(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := db.InitDatabase(t.TempDir()); err != nil {
				t.Fatalf("init database: %v", err)
			}
			handler := NewHandler()

			create := performJSONRequest(t, handler, http.MethodPost, tt.collection, tt.createBody)
			if create.Code != http.StatusCreated {
				t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
			}
			created := decodeBody(t, create)
			if got := created[tt.valueField].(string); got != tt.emptySection {
				t.Fatalf("created %s = %q, want %q", tt.valueField, got, tt.emptySection)
			}

			id := int(created["id"].(float64))
			reset := performJSONRequest(t, handler, http.MethodPut, fmt.Sprintf("%s/%d", tt.collection, id), tt.resetBody)
			if reset.Code != http.StatusOK {
				t.Fatalf("reset status = %d, body = %s", reset.Code, reset.Body.String())
			}
			updated := decodeBody(t, reset)
			if got := updated[tt.valueField].(string); got != tt.emptySection {
				t.Fatalf("reset %s = %q, want %q", tt.valueField, got, tt.emptySection)
			}
		})
	}
}

func TestConfigStructuredGlobalInputHandlers(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	handler := NewHandler()

	create := performJSONRequest(t, handler, http.MethodPost, "/configs", `{"name":"cfg-structured","parsedGlobal":{"logLevel":"debug","tproxyPort":23456,"tcpCheckUrl":["https://example.com/check"]}}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create structured config status = %d, body = %s", create.Code, create.Body.String())
	}
	created := decodeBody(t, create)
	parsedGlobal := created["parsedGlobal"].(map[string]any)
	if got := parsedGlobal["logLevel"].(string); got != "debug" {
		t.Fatalf("parsedGlobal.logLevel = %q, want debug", got)
	}
	if got := int(parsedGlobal["tproxyPort"].(float64)); got != 23456 {
		t.Fatalf("parsedGlobal.tproxyPort = %d, want 23456", got)
	}
	tcpCheckURLs := parsedGlobal["tcpCheckUrl"].([]any)
	if len(tcpCheckURLs) != 1 || tcpCheckURLs[0].(string) != "https://example.com/check" {
		t.Fatalf("parsedGlobal.tcpCheckUrl = %#v", parsedGlobal["tcpCheckUrl"])
	}

	id := int(created["id"].(float64))
	update := performJSONRequest(t, handler, http.MethodPut, "/configs/"+itoa(id), `{"parsedGlobal":{"logLevel":"warn","disableWaitingNetwork":true}}`)
	if update.Code != http.StatusOK {
		t.Fatalf("update structured config status = %d, body = %s", update.Code, update.Body.String())
	}
	updated := decodeBody(t, update)
	updatedGlobal := updated["parsedGlobal"].(map[string]any)
	if got := updatedGlobal["logLevel"].(string); got != "warn" {
		t.Fatalf("updated parsedGlobal.logLevel = %q, want warn", got)
	}
	if got := updatedGlobal["disableWaitingNetwork"].(bool); !got {
		t.Fatalf("updated parsedGlobal.disableWaitingNetwork = false, want true")
	}
	if got := int(updatedGlobal["tproxyPort"].(float64)); got != 23456 {
		t.Fatalf("updated parsedGlobal.tproxyPort = %d, want 23456", got)
	}
}

func TestConfigExpandParsedToleratesBrokenStoredGlobal(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	if err := db.DB(context.Background()).Create(&db.Config{
		Name:   "broken-config",
		Global: "global {\n  log_level: \n}",
	}).Error; err != nil {
		t.Fatalf("seed broken config: %v", err)
	}

	handler := NewHandler()

	list := performJSONRequest(t, handler, http.MethodGet, "/configs?expand=parsed", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(listed.Items))
	}
	if got := listed.Items[0]["global"].(string); !strings.Contains(got, "log_level") {
		t.Fatalf("global = %q, want raw broken body", got)
	}
	if _, ok := listed.Items[0]["parsedGlobal"]; ok {
		t.Fatalf("parsedGlobal should be omitted when parsing fails")
	}
	if got, ok := listed.Items[0]["parseError"].(string); !ok || got == "" {
		t.Fatalf("parseError = %#v, want non-empty string", listed.Items[0]["parseError"])
	}

	id := int(listed.Items[0]["id"].(float64))
	get := performJSONRequest(t, handler, http.MethodGet, "/configs/"+itoa(id), "")
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", get.Code, get.Body.String())
	}
	fetched := decodeBody(t, get)
	if _, ok := fetched["parsedGlobal"]; ok {
		t.Fatalf("parsedGlobal should be omitted on single read when parsing fails")
	}
	if got, ok := fetched["parseError"].(string); !ok || got == "" {
		t.Fatalf("single parseError = %#v, want non-empty string", fetched["parseError"])
	}
}

func TestParsedConfigPreviewHandlers(t *testing.T) {
	handler := NewHandler()

	rawPreview := performJSONRequest(t, handler, http.MethodPost, "/configs/parsed", `{"global":"global {\n  log_level: debug\n  tproxy_port: 12345\n}"}`)
	if rawPreview.Code != http.StatusOK {
		t.Fatalf("raw preview status = %d, body = %s", rawPreview.Code, rawPreview.Body.String())
	}
	rawBody := decodeBody(t, rawPreview)
	if got := rawBody["global"].(string); !strings.Contains(got, "tproxy_port: 12345") {
		t.Fatalf("preview global = %q, want normalized raw body", got)
	}
	parsedGlobal := rawBody["parsedGlobal"].(map[string]any)
	if got := parsedGlobal["logLevel"].(string); got != "debug" {
		t.Fatalf("preview parsedGlobal.logLevel = %q, want debug", got)
	}

	parsedPreview := performJSONRequest(t, handler, http.MethodPost, "/configs/parsed", `{"parsedGlobal":{"logLevel":"warn","disableWaitingNetwork":true}}`)
	if parsedPreview.Code != http.StatusOK {
		t.Fatalf("structured preview status = %d, body = %s", parsedPreview.Code, parsedPreview.Body.String())
	}
	parsedBody := decodeBody(t, parsedPreview)
	if got := parsedBody["global"].(string); !strings.Contains(got, "log_level:") || !strings.Contains(got, "disable_waiting_network:") {
		t.Fatalf("structured preview global = %q, want marshaled body", got)
	}
	structuredParsed := parsedBody["parsedGlobal"].(map[string]any)
	if got := structuredParsed["disableWaitingNetwork"].(bool); !got {
		t.Fatalf("structured parsedGlobal.disableWaitingNetwork = false, want true")
	}
}

func TestParsedPreviewHandlers(t *testing.T) {
	handler := NewHandler()

	routing := performJSONRequest(t, handler, http.MethodPost, "/routings/parsed", `{"raw":"domain(geosite:cn) -> direct\nfallback: proxy"}`)
	if routing.Code != http.StatusOK {
		t.Fatalf("parsed routing status = %d, body = %s", routing.Code, routing.Body.String())
	}
	parsedRouting := decodeBody(t, routing)
	rules, ok := parsedRouting["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("parsed routing rules = %#v", parsedRouting["rules"])
	}
	rule := rules[0].(map[string]any)
	outbound := rule["outbound"].(map[string]any)
	if got := outbound["name"].(string); got != "direct" {
		t.Fatalf("parsed routing outbound = %q, want direct", got)
	}
	fallback := parsedRouting["fallback"].(map[string]any)
	if got := fallback["type"].(string); got != "plaintext" {
		t.Fatalf("parsed routing fallback type = %q, want plaintext", got)
	}
	if got := fallback["plaintext"].(string); got != "proxy" {
		t.Fatalf("parsed routing fallback plaintext = %q, want proxy", got)
	}

	dnsRaw := `{"raw":"upstream {\n  googledns: 'tcp+udp://dns.google:53'\n}\nrouting {\n  request {\n    fallback: googledns\n  }\n}"}`
	dns := performJSONRequest(t, handler, http.MethodPost, "/dns/parsed", dnsRaw)
	if dns.Code != http.StatusOK {
		t.Fatalf("parsed dns status = %d, body = %s", dns.Code, dns.Body.String())
	}
	parsedDNS := decodeBody(t, dns)
	upstream, ok := parsedDNS["upstream"].([]any)
	if !ok || len(upstream) != 1 {
		t.Fatalf("parsed dns upstream = %#v", parsedDNS["upstream"])
	}
	firstUpstream := upstream[0].(map[string]any)
	if got := firstUpstream["key"].(string); got != "googledns" {
		t.Fatalf("parsed dns upstream key = %q, want googledns", got)
	}
	dnsRouting := parsedDNS["routing"].(map[string]any)
	requestRouting := dnsRouting["request"].(map[string]any)
	requestFallback := requestRouting["fallback"].(map[string]any)
	if got := requestFallback["plaintext"].(string); got != "googledns" {
		t.Fatalf("parsed dns request fallback = %q, want googledns", got)
	}
}

func performJSONRequest(t *testing.T, handler http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), "user", &db.User{}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}
