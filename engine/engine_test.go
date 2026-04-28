package engine

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestNativeServiceDryRunLifecycle(t *testing.T) {
	svc := nativeService{}
	log := logrus.New()

	done := make(chan error, 1)
	go func() {
		done <- svc.Run(log, svc.EmptyConfig(), nil, true, true)
	}()

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
}
