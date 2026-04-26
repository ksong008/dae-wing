package defaults

import (
	"context"
	"sync"
	"testing"

	"github.com/daeuniverse/dae-wing/db"
)

func TestEnsureIsIdempotentAndAtomicPerProcess(t *testing.T) {
	dir := t.TempDir()
	if err := db.InitDatabase(dir); err != nil {
		t.Fatalf("InitDatabase: %v", err)
	}

	user := db.User{
		Username:     "tester",
		PasswordHash: "hash",
		JwtSecret:    "secret",
		JsonStorage:  "{}",
	}
	if err := db.DB(context.Background()).Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	input := EnsureInput{
		ConfigName:   "global",
		Global:       "global {\nlog_level: 'info'\nwan_interface: 'auto'\n}",
		DnsName:      "default",
		Dns:          "upstream { googledns: 'udp://8.8.8.8:53' }\nrouting { request { fallback: googledns } }",
		RoutingName:  "default",
		Routing:      "fallback: direct",
		GroupName:    "proxy",
		Policy:       "min_moving_avg",
		PolicyParams: nil,
		Mode:         "simple",
	}

	results := make([]*EnsureResult, 4)
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range results {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = Ensure(context.Background(), user.ID, input)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Ensure[%d]: %v", i, err)
		}
	}
	for i := 1; i < len(results); i++ {
		if results[i].DefaultConfigID != results[0].DefaultConfigID ||
			results[i].DefaultRoutingID != results[0].DefaultRoutingID ||
			results[i].DefaultDNSID != results[0].DefaultDNSID ||
			results[i].DefaultGroupID != results[0].DefaultGroupID {
			t.Fatalf("Ensure returned inconsistent IDs: %#v vs %#v", results[0], results[i])
		}
	}

	assertCount(t, &db.Config{}, 1)
	assertCount(t, &db.Routing{}, 1)
	assertCount(t, &db.Dns{}, 1)
	assertCount(t, &db.Group{}, 1)

	var configSelected int64
	if err := db.DB(context.Background()).Model(&db.Config{}).Where("selected = ?", true).Count(&configSelected).Error; err != nil {
		t.Fatalf("count selected config: %v", err)
	}
	if configSelected != 1 {
		t.Fatalf("expected 1 selected config, got %d", configSelected)
	}

	var routingSelected int64
	if err := db.DB(context.Background()).Model(&db.Routing{}).Where("selected = ?", true).Count(&routingSelected).Error; err != nil {
		t.Fatalf("count selected routing: %v", err)
	}
	if routingSelected != 1 {
		t.Fatalf("expected 1 selected routing, got %d", routingSelected)
	}

	var dnsSelected int64
	if err := db.DB(context.Background()).Model(&db.Dns{}).Where("selected = ?", true).Count(&dnsSelected).Error; err != nil {
		t.Fatalf("count selected dns: %v", err)
	}
	if dnsSelected != 1 {
		t.Fatalf("expected 1 selected dns, got %d", dnsSelected)
	}
}

func TestEnsureReusesExistingResourcesReferencedByStorage(t *testing.T) {
	dir := t.TempDir()
	if err := db.InitDatabase(dir); err != nil {
		t.Fatalf("InitDatabase: %v", err)
	}

	config := db.Config{Name: "global", Global: "global {\nlog_level: 'info'\nwan_interface: 'auto'\n}", Selected: true}
	routing := db.Routing{Name: "default", Routing: wrapSection("routing", "fallback: direct"), Selected: true}
	dns := db.Dns{Name: "default", Dns: wrapSection("dns", "upstream { googledns: 'udp://8.8.8.8:53' }\nrouting { request { fallback: googledns } }"), Selected: true}
	group := db.Group{Name: "proxy", Policy: "min_moving_avg"}

	ctx := context.Background()
	for _, record := range []any{&config, &routing, &dns, &group} {
		if err := db.DB(ctx).Create(record).Error; err != nil {
			t.Fatalf("seed %T: %v", record, err)
		}
	}

	user := db.User{
		Username:     "tester",
		PasswordHash: "hash",
		JwtSecret:    "secret",
		JsonStorage:  `{"defaultConfigID":"` + encodeCursor(config.ID) + `","defaultRoutingID":"` + encodeCursor(routing.ID) + `","defaultDNSID":"` + encodeCursor(dns.ID) + `","defaultGroupID":"` + encodeCursor(group.ID) + `","mode":"rule"}`,
	}
	if err := db.DB(ctx).Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	result, err := Ensure(ctx, user.ID, EnsureInput{
		ConfigName:  config.Name,
		Global:      config.Global,
		DnsName:     dns.Name,
		Dns:         dns.Dns,
		RoutingName: routing.Name,
		Routing:     routing.Routing,
		GroupName:   group.Name,
		Policy:      group.Policy,
		Mode:        "simple",
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if result.DefaultConfigID != config.ID || result.DefaultRoutingID != routing.ID || result.DefaultDNSID != dns.ID || result.DefaultGroupID != group.ID {
		t.Fatalf("Ensure reused unexpected IDs: %#v", result)
	}
	if result.Mode != "rule" {
		t.Fatalf("expected existing mode to be preserved, got %q", result.Mode)
	}

	assertCount(t, &db.Config{}, 1)
	assertCount(t, &db.Routing{}, 1)
	assertCount(t, &db.Dns{}, 1)
	assertCount(t, &db.Group{}, 1)
}

func assertCount(t *testing.T, model any, want int64) {
	t.Helper()
	var count int64
	if err := db.DB(context.Background()).Model(model).Count(&count).Error; err != nil {
		t.Fatalf("count %T: %v", model, err)
	}
	if count != want {
		t.Fatalf("expected %d %T records, got %d", want, model, count)
	}
}
