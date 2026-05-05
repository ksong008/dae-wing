package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/control"
	"github.com/sirupsen/logrus"
)

func TestRuntimeLifecycleRejectsStopDuringReload(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}
	seedRunnableConfig(t)

	svc := newBlockingReloadService()
	engine.SetDefault(svc)
	t.Cleanup(func() {
		svc.release()
		engine.SetDefault(nil)
	})

	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), false)
		done <- err
	}()

	select {
	case <-svc.reloadStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reload to start")
	}

	err := Stop(context.Background(), time.Second)
	if !errors.Is(err, errRuntimeOperationInProgress) {
		t.Fatalf("Stop() error = %v, want runtime operation in progress", err)
	}
	if calls := svc.stopCalls.Load(); calls != 0 {
		t.Fatalf("Stop() reached engine while reload was in progress, calls = %d", calls)
	}

	svc.release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for run to complete")
	}

	var sys db.System
	if err := db.DB(context.Background()).First(&sys).Error; err != nil {
		t.Fatalf("load system: %v", err)
	}
	if !sys.Running {
		t.Fatal("runtime should remain marked running after rejected concurrent stop")
	}
}

func seedRunnableConfig(t *testing.T) {
	t.Helper()

	if err := db.DB(context.Background()).Create(&db.Config{Name: "cfg", Global: "global {}", Selected: true}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.DB(context.Background()).Create(&db.Dns{Name: "dns", Dns: "dns {}", Selected: true}).Error; err != nil {
		t.Fatalf("seed dns: %v", err)
	}
	if err := db.DB(context.Background()).Create(&db.Routing{Name: "routing", Routing: "routing {}", Selected: true}).Error; err != nil {
		t.Fatalf("seed routing: %v", err)
	}

	node := db.Node{Link: "direct://", Name: "Direct", Address: "direct", Protocol: "direct"}
	if err := db.DB(context.Background()).Create(&node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	group := db.Group{Name: "proxy", Policy: "random"}
	if err := db.DB(context.Background()).Create(&group).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := db.DB(context.Background()).Model(&group).Association("Node").Append(&node); err != nil {
		t.Fatalf("seed group node: %v", err)
	}
}

type blockingReloadService struct {
	reloadStarted chan struct{}
	reloadRelease chan struct{}
	startOnce     sync.Once
	releaseOnce   sync.Once
	stopCalls     atomic.Int32
}

func newBlockingReloadService() *blockingReloadService {
	return &blockingReloadService{
		reloadStarted: make(chan struct{}),
		reloadRelease: make(chan struct{}),
	}
}

func (s *blockingReloadService) release() {
	s.releaseOnce.Do(func() {
		close(s.reloadRelease)
	})
}

func (s *blockingReloadService) EmptyGlobalSection() string {
	return "global {}"
}

func (s *blockingReloadService) EmptyDnsSection() string {
	return "dns {}"
}

func (s *blockingReloadService) EmptyRoutingSection() string {
	return "routing {}"
}

func (s *blockingReloadService) EmptyConfig() *daeConfig.Config {
	return &daeConfig.Config{}
}

func (s *blockingReloadService) ExportFlatDesc() []*engine.FlatDesc {
	return nil
}

func (s *blockingReloadService) ParseConfig(globalSection *string, dnsSection *string, routingSection *string) (*daeConfig.Config, error) {
	return &daeConfig.Config{}, nil
}

func (s *blockingReloadService) NecessaryOutbounds(routing *daeConfig.Routing) []string {
	return []string{"proxy"}
}

func (s *blockingReloadService) Run(log *logrus.Logger, conf *daeConfig.Config, externGeoDataDirs []string, disableTimestamp bool, dry bool) error {
	return nil
}

func (s *blockingReloadService) Reload(conf *daeConfig.Config) error {
	return s.ReloadContext(context.Background(), conf)
}

func (s *blockingReloadService) ReloadContext(ctx context.Context, conf *daeConfig.Config) error {
	s.startOnce.Do(func() {
		close(s.reloadStarted)
	})
	select {
	case <-s.reloadRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *blockingReloadService) Stop(timeout time.Duration) error {
	s.stopCalls.Add(1)
	return nil
}

func (s *blockingReloadService) ControlPlane() (*control.ControlPlane, error) {
	return nil, nil
}

func (s *blockingReloadService) GetRuntimeOverview(windowSec int, maxPoints int) (*engine.RuntimeOverview, error) {
	return &engine.RuntimeOverview{}, nil
}

func (s *blockingReloadService) HTTPTransport() http.RoundTripper {
	return http.DefaultTransport
}

func (s *blockingReloadService) IsControlPlaneNotInit(err error) bool {
	return false
}
