/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package engine

import daeengine "github.com/daeuniverse/dae/engine"

type RuntimeTrafficSample = daeengine.RuntimeTrafficSample
type RuntimeOverview = daeengine.RuntimeOverview
type FlatDesc = daeengine.FlatDesc
type Service = daeengine.Service
type nativeService = daeengine.NativeService

func newNativeService() *nativeService {
	return daeengine.NewNativeService(daeengine.Options{})
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
