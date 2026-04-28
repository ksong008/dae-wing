package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
)

type benchmarkFixture struct {
	handler        http.Handler
	user           *db.User
	subscriptionID uint
}

func BenchmarkHTTPAPIConfigsListRaw(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	benchmarkRequest(b, fixture.handler, fixture.user, http.MethodGet, "/configs")
}

func BenchmarkHTTPAPIConfigsListExpandParsed(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	benchmarkRequest(b, fixture.handler, fixture.user, http.MethodGet, "/configs?expand=parsed")
}

func BenchmarkHTTPAPIGroupsList(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	benchmarkRequest(b, fixture.handler, fixture.user, http.MethodGet, "/groups")
}

func BenchmarkHTTPAPINodesListDefault(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	benchmarkRequest(b, fixture.handler, fixture.user, http.MethodGet, "/nodes")
}

func BenchmarkHTTPAPISubscriptionNodesFirst(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	benchmarkRequest(b, fixture.handler, fixture.user, http.MethodGet, fmt.Sprintf("/subscriptions/%d/nodes?first=32", fixture.subscriptionID))
}

func newBenchmarkFixture(b *testing.B) benchmarkFixture {
	b.Helper()

	if err := db.InitDatabase(b.TempDir()); err != nil {
		b.Fatalf("init database: %v", err)
	}

	ctx := context.Background()

	configs := make([]db.Config, 0, 128)
	for i := 0; i < 128; i++ {
		configs = append(configs, db.Config{
			Name:     fmt.Sprintf("cfg-%03d", i),
			Global:   benchmarkGlobalSection(i),
			Selected: i == 0,
		})
	}
	if err := db.DB(ctx).Create(&configs).Error; err != nil {
		b.Fatalf("seed configs: %v", err)
	}

	independentNodes := make([]db.Node, 0, 256)
	for i := 0; i < 256; i++ {
		independentNodes = append(independentNodes, db.Node{
			Link:     fmt.Sprintf("ss://independent-%03d", i),
			Name:     fmt.Sprintf("Independent %03d", i),
			Address:  fmt.Sprintf("10.0.%d.%d", i/256, i%256),
			Protocol: "ss",
		})
	}
	if err := db.DB(ctx).Create(&independentNodes).Error; err != nil {
		b.Fatalf("seed independent nodes: %v", err)
	}

	subscriptions := make([]db.Subscription, 0, 8)
	for i := 0; i < 8; i++ {
		subscriptions = append(subscriptions, db.Subscription{
			UpdatedAt:  time.Now(),
			Link:       fmt.Sprintf("https://example.invalid/sub-%02d", i),
			CronExp:    "10 */6 * * *",
			CronEnable: true,
			Status:     "",
			Info:       "",
		})
	}
	if err := db.DB(ctx).Create(&subscriptions).Error; err != nil {
		b.Fatalf("seed subscriptions: %v", err)
	}

	subscriptionNodes := make([]db.Node, 0, len(subscriptions)*24)
	for i := range subscriptions {
		for j := 0; j < 24; j++ {
			subscriptionNodes = append(subscriptionNodes, db.Node{
				Link:           fmt.Sprintf("ss://sub-%02d-node-%02d", i, j),
				Name:           fmt.Sprintf("Sub %02d Node %02d", i, j),
				Address:        fmt.Sprintf("172.16.%d.%d", i, j),
				Protocol:       "ss",
				SubscriptionID: &subscriptions[i].ID,
			})
		}
	}
	if err := db.DB(ctx).Create(&subscriptionNodes).Error; err != nil {
		b.Fatalf("seed subscription nodes: %v", err)
	}

	for i := 0; i < 12; i++ {
		group := db.Group{Name: fmt.Sprintf("group-%02d", i), Policy: "random"}
		if err := db.DB(ctx).Create(&group).Error; err != nil {
			b.Fatalf("seed group %d: %v", i, err)
		}
		start := (i * 8) % len(independentNodes)
		nodesToAttach := make([]*db.Node, 0, 8)
		for j := 0; j < 8; j++ {
			node := &independentNodes[(start+j)%len(independentNodes)]
			nodesToAttach = append(nodesToAttach, node)
		}
		if err := db.DB(ctx).Model(&group).Association("Node").Append(nodesToAttach); err != nil {
			b.Fatalf("attach group nodes %d: %v", i, err)
		}

		for j := 0; j < 2; j++ {
			sub := subscriptions[(i+j)%len(subscriptions)]
			binding := db.GroupSubscription{
				GroupID:        group.ID,
				SubscriptionID: sub.ID,
			}
			if err := db.DB(ctx).Create(&binding).Error; err != nil {
				b.Fatalf("seed group subscription %d/%d: %v", i, j, err)
			}
		}
	}

	return benchmarkFixture{
		handler:        NewHandler(),
		user:           &db.User{},
		subscriptionID: subscriptions[0].ID,
	}
}

func benchmarkRequest(b *testing.B, handler http.Handler, user *db.User, method string, path string) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(method, path, nil)
		if user != nil {
			req = req.WithContext(context.WithValue(req.Context(), "user", user))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
}

func benchmarkGlobalSection(i int) string {
	return fmt.Sprintf(`global {
  log_level: 'info'
  tcp_check_url: 'https://example-%03d.invalid/check'
  udp_check_dns: '1.1.1.1:53'
  check_interval: '30s'
  sniffing_timeout: '100ms'
}`, i)
}
