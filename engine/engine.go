/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package engine

import (
	"net/http"
	"time"

	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/control"
	"github.com/daeuniverse/dae/engine"
	"github.com/sirupsen/logrus"
)

type RuntimeTrafficSample = engine.RuntimeTrafficSample
type RuntimeOverview = engine.RuntimeOverview
type FlatDesc = engine.FlatDesc

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
	Stop(timeout time.Duration) error
	ControlPlane() (*control.ControlPlane, error)
	GetRuntimeOverview(windowSec int, maxPoints int) (*RuntimeOverview, error)
	HTTPTransport() http.RoundTripper
	IsControlPlaneNotInit(err error) bool
}

type nativeService struct {
	engine *engine.Engine
}

func newNativeService() *nativeService {
	return &nativeService{
		engine: engine.New(engine.Options{}),
	}
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
	return engine.EmptyGlobalSection
}

func (*nativeService) EmptyDnsSection() string {
	return engine.EmptyDnsSection
}

func (*nativeService) EmptyRoutingSection() string {
	return engine.EmptyRoutingSection
}

func (*nativeService) EmptyConfig() *daeConfig.Config {
	return engine.EmptyConfig()
}

func (*nativeService) ExportFlatDesc() []*FlatDesc {
	return engine.ExportFlatDesc()
}

func (*nativeService) ParseConfig(globalSection *string, dnsSection *string, routingSection *string) (*daeConfig.Config, error) {
	return engine.ParseConfig(globalSection, dnsSection, routingSection)
}

func (*nativeService) NecessaryOutbounds(routing *daeConfig.Routing) []string {
	return engine.NecessaryOutbounds(routing)
}

func (n *nativeService) Run(log *logrus.Logger, conf *daeConfig.Config, externGeoDataDirs []string, disableTimestamp bool, dry bool) error {
	return n.engine.Run(log, conf, externGeoDataDirs, disableTimestamp, dry)
}

func (n *nativeService) Reload(conf *daeConfig.Config) error {
	return n.engine.Reload(conf)
}

func (n *nativeService) Stop(timeout time.Duration) error {
	return n.engine.Stop(timeout)
}

func (n *nativeService) ControlPlane() (*control.ControlPlane, error) {
	return n.engine.ControlPlane()
}

func (n *nativeService) GetRuntimeOverview(windowSec int, maxPoints int) (*RuntimeOverview, error) {
	return n.engine.GetRuntimeOverview(windowSec, maxPoints)
}

func (n *nativeService) HTTPTransport() http.RoundTripper {
	return n.engine.HTTPTransport()
}

func (n *nativeService) IsControlPlaneNotInit(err error) bool {
	return n.engine.IsControlPlaneNotInit(err)
}
