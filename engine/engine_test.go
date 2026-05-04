package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestNativeServiceDryRunLifecycle(t *testing.T) {
	svc := newNativeService()
	log := logrus.New()

	done := make(chan error, 1)
	go func() {
		done <- svc.Run(log, svc.EmptyConfig(), nil, true, true)
	}()
	waitNativeServiceRunning(t, svc)

	if err := svc.Reload(svc.EmptyConfig()); err != nil {
		t.Fatalf("reload in dry mode: %v", err)
	}

	overview, err := svc.GetRuntimeOverview(60, 16)
	if err != nil {
		t.Fatalf("runtime overview: %v", err)
	}
	if overview == nil {
		t.Fatal("runtime overview is nil")
	}

	if err := svc.Stop(2 * time.Second); err != nil {
		t.Fatalf("stop engine: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dry-run engine to exit")
	}

	if err := svc.Reload(svc.EmptyConfig()); err != nil {
		t.Fatalf("reload after stop should restart dry runtime: %v", err)
	}
	if err := svc.Stop(2 * time.Second); err != nil {
		t.Fatalf("stop restarted dry runtime: %v", err)
	}
}

func TestNativeServiceReloadContextCanceledBeforeStart(t *testing.T) {
	svc := newNativeService()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.ReloadContext(ctx, svc.EmptyConfig())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReloadContext() error = %v, want context canceled", err)
	}
	if _, running := svc.currentRuntime(); running {
		t.Fatal("runtime should not start after canceled reload context")
	}
}

func waitNativeServiceRunning(t *testing.T, svc *nativeService) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for native service to run")
		case <-ticker.C:
			if _, running := svc.currentRuntime(); running {
				return
			}
		}
	}
}
