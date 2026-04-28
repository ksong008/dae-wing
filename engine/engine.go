/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package engine

import (
	"errors"
	"net/http"
	"time"

	"github.com/daeuniverse/dae-wing/dae"
	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/control"
	"github.com/sirupsen/logrus"
)

type RuntimeTrafficSample = dae.RuntimeTrafficSample
type RuntimeOverview = dae.RuntimeOverview
type FlatDesc = dae.FlatDesc

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

type nativeService struct{}

var defaultService Service = nativeService{}

func Default() Service {
	return defaultService
}

func SetDefault(service Service) {
	if service == nil {
		defaultService = nativeService{}
		return
	}
	defaultService = service
}

func (nativeService) EmptyGlobalSection() string {
	return dae.EmptyGlobalSection
}

func (nativeService) EmptyDnsSection() string {
	return dae.EmptyDnsSection
}

func (nativeService) EmptyRoutingSection() string {
	return dae.EmptyRoutingSection
}

func (nativeService) EmptyConfig() *daeConfig.Config {
	return dae.EmptyConfig
}

func (nativeService) ExportFlatDesc() []*FlatDesc {
	return dae.ExportFlatDesc()
}

func (nativeService) ParseConfig(globalSection *string, dnsSection *string, routingSection *string) (*daeConfig.Config, error) {
	return dae.ParseConfig(globalSection, dnsSection, routingSection)
}

func (nativeService) NecessaryOutbounds(routing *daeConfig.Routing) []string {
	return dae.NecessaryOutbounds(routing)
}

func (nativeService) Run(log *logrus.Logger, conf *daeConfig.Config, externGeoDataDirs []string, disableTimestamp bool, dry bool) error {
	return dae.Run(log, conf, externGeoDataDirs, disableTimestamp, dry)
}

func (nativeService) Reload(conf *daeConfig.Config) error {
	ch := make(chan error, 1)
	dae.ChReloadConfigs <- &dae.ReloadMessage{
		Config:   conf,
		Callback: ch,
	}
	return <-ch
}

func (nativeService) Stop(timeout time.Duration) error {
	if timeout <= 0 {
		dae.ChReloadConfigs <- nil
		<-dae.GracefullyExit
		return nil
	}
	select {
	case dae.ChReloadConfigs <- nil:
	case <-time.After(timeout):
		return errors.New("timeout sending dae shutdown signal")
	}
	select {
	case <-dae.GracefullyExit:
		return nil
	case <-time.After(timeout):
		return errors.New("timeout waiting for dae shutdown")
	}
}

func (nativeService) ControlPlane() (*control.ControlPlane, error) {
	return dae.ControlPlane()
}

func (nativeService) GetRuntimeOverview(windowSec int, maxPoints int) (*RuntimeOverview, error) {
	return dae.GetRuntimeOverview(windowSec, maxPoints)
}

func (nativeService) HTTPTransport() http.RoundTripper {
	return dae.HttpTransport
}

func (nativeService) IsControlPlaneNotInit(err error) bool {
	return errors.Is(err, dae.ErrControlPlaneNotInit)
}
