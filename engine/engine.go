/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package engine

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/control"
	daeengine "github.com/daeuniverse/dae/engine"
	"github.com/sirupsen/logrus"
)

type RuntimeTrafficSample = daeengine.RuntimeTrafficSample
type RuntimeOverview = daeengine.RuntimeOverview
type FlatDesc = daeengine.FlatDesc

const timedOutStartStopTimeout = 5 * time.Second

type Service interface {
	EmptyGlobalSection() string
	EmptyDnsSection() string
	EmptyRoutingSection() string
	EmptyConfig() *daeConfig.Config
	ExportFlatDesc() []*FlatDesc
	ParseConfig(globalSection *string, dnsSection *string, routingSection *string) (*daeConfig.Config, error)
	NecessaryOutbounds(routing *daeConfig.Routing) []string
	Run(log *logrus.Logger, conf *daeConfig.Config, externGeoDataDirs []string, disableTimestamp bool, dry bool) error
	Reload(conf *daeConfig.Config) error
	ReloadContext(ctx context.Context, conf *daeConfig.Config) error
	Stop(timeout time.Duration) error
	ControlPlane() (*control.ControlPlane, error)
	GetRuntimeOverview(windowSec int, maxPoints int) (*RuntimeOverview, error)
	HTTPTransport() http.RoundTripper
	IsControlPlaneNotInit(err error) bool
}

type nativeService struct {
	mu      sync.RWMutex
	startMu sync.Mutex

	engine  *daeengine.Engine
	running bool

	log               *logrus.Logger
	externGeoDataDirs []string
	disableTimestamp  bool
	dry               bool
}

func newNativeService() *nativeService {
	return &nativeService{}
}

var defaultService Service = newNativeService()

func Default() Service {
	return defaultService
}

func SetDefault(service Service) {
	if service == nil {
		defaultService = newNativeService()
		return
	}
	defaultService = service
}

func (*nativeService) EmptyGlobalSection() string {
	return daeengine.EmptyGlobalSection
}

func (*nativeService) EmptyDnsSection() string {
	return daeengine.EmptyDnsSection
}

func (*nativeService) EmptyRoutingSection() string {
	return daeengine.EmptyRoutingSection
}

func (*nativeService) EmptyConfig() *daeConfig.Config {
	return daeengine.EmptyConfig()
}

func (*nativeService) ExportFlatDesc() []*FlatDesc {
	return daeengine.ExportFlatDesc()
}

func (*nativeService) ParseConfig(globalSection *string, dnsSection *string, routingSection *string) (*daeConfig.Config, error) {
	return daeengine.ParseConfig(globalSection, dnsSection, routingSection)
}

func (*nativeService) NecessaryOutbounds(routing *daeConfig.Routing) []string {
	return daeengine.NecessaryOutbounds(routing)
}

func (n *nativeService) Run(log *logrus.Logger, conf *daeConfig.Config, externGeoDataDirs []string, disableTimestamp bool, dry bool) error {
	runtime := daeengine.New(daeengine.Options{})
	n.markRunning(runtime, log, externGeoDataDirs, disableTimestamp, dry)
	err := runtime.Run(log, conf, externGeoDataDirs, disableTimestamp, dry)
	n.markStopped(runtime)
	return err
}

func (n *nativeService) Reload(conf *daeConfig.Config) error {
	return n.ReloadContext(context.Background(), conf)
}

func (n *nativeService) ReloadContext(ctx context.Context, conf *daeConfig.Config) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime, running := n.currentRuntime()
	if running && runtime != nil {
		return runtime.ReloadWithContext(ctx, conf)
	}

	n.startMu.Lock()
	defer n.startMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	runtime, running = n.currentRuntime()
	if running && runtime != nil {
		return runtime.ReloadWithContext(ctx, conf)
	}
	return n.startRuntime(ctx, conf)
}

func (n *nativeService) Stop(timeout time.Duration) error {
	runtime, running := n.currentRuntime()
	if !running || runtime == nil {
		return nil
	}
	return runtime.Stop(timeout)
}

func (n *nativeService) ControlPlane() (*control.ControlPlane, error) {
	runtime, running := n.currentRuntime()
	if !running || runtime == nil {
		return nil, daeengine.ErrControlPlaneNotInit
	}
	return runtime.ControlPlane()
}

func (n *nativeService) GetRuntimeOverview(windowSec int, maxPoints int) (*RuntimeOverview, error) {
	runtime, running := n.currentRuntime()
	if !running || runtime == nil {
		return (&daeengine.Engine{}).GetRuntimeOverview(windowSec, maxPoints)
	}
	return runtime.GetRuntimeOverview(windowSec, maxPoints)
}

func (n *nativeService) HTTPTransport() http.RoundTripper {
	runtime, running := n.currentRuntime()
	if !running || runtime == nil {
		return notInitializedTransport{}
	}
	return runtime.HTTPTransport()
}

func (n *nativeService) IsControlPlaneNotInit(err error) bool {
	return errors.Is(err, daeengine.ErrControlPlaneNotInit)
}

func (n *nativeService) markRunning(runtime *daeengine.Engine, log *logrus.Logger, externGeoDataDirs []string, disableTimestamp bool, dry bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.engine = runtime
	n.running = true
	n.log = log
	n.externGeoDataDirs = append([]string(nil), externGeoDataDirs...)
	n.disableTimestamp = disableTimestamp
	n.dry = dry
}

func (n *nativeService) markStopped(runtime *daeengine.Engine) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.engine != runtime {
		return
	}
	n.engine = nil
	n.running = false
}

func (n *nativeService) currentRuntime() (*daeengine.Engine, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.engine, n.running
}

func (n *nativeService) startRuntime(ctx context.Context, conf *daeConfig.Config) error {
	n.mu.RLock()
	log := n.log
	externGeoDataDirs := append([]string(nil), n.externGeoDataDirs...)
	disableTimestamp := n.disableTimestamp
	dry := n.dry
	n.mu.RUnlock()
	if log == nil {
		log = logrus.New()
	}

	ready := make(chan struct{}, 1)
	runtime := daeengine.New(daeengine.Options{
		OnReady: func() {
			select {
			case ready <- struct{}{}:
			default:
			}
		},
	})
	runErr := make(chan error, 1)
	n.markRunning(runtime, log, externGeoDataDirs, disableTimestamp, dry)

	go func() {
		err := runtime.Run(log, conf, externGeoDataDirs, disableTimestamp, dry)
		n.markStopped(runtime)
		runErr <- err
		close(runErr)
	}()

	if dry {
		return nil
	}

	select {
	case <-ready:
		return nil
	case err := <-runErr:
		if err == nil {
			return daeengine.ErrControlPlaneNotInit
		}
		return err
	case <-ctx.Done():
		go func() {
			if err := runtime.Stop(timedOutStartStopTimeout); err != nil {
				log.WithError(err).Warnln("failed to stop runtime after start timeout")
			}
		}()
		return ctx.Err()
	}
}

type notInitializedTransport struct{}

func (notInitializedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, daeengine.ErrControlPlaneNotInit
}
