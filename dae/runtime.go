/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package dae

import (
	"errors"
	"sync"
	"time"

	"github.com/daeuniverse/dae/control"
)

type RuntimeTrafficSample = control.RuntimeTrafficSample

type RuntimeOverview = control.RuntimeStatsSnapshot

const runtimeOverviewCacheTTL = 500 * time.Millisecond

var runtimeOverviewCache struct {
	sync.Mutex
	windowSec int
	maxPoints int
	expiresAt time.Time
	snapshot  RuntimeOverview
}

func GetRuntimeOverview(windowSec int, maxPoints int) (*RuntimeOverview, error) {
	now := time.Now()
	runtimeOverviewCache.Lock()
	if runtimeOverviewCache.windowSec == windowSec && runtimeOverviewCache.maxPoints == maxPoints && now.Before(runtimeOverviewCache.expiresAt) {
		snapshot := runtimeOverviewCache.snapshot
		runtimeOverviewCache.Unlock()
		return &snapshot, nil
	}
	runtimeOverviewCache.Unlock()

	activeTCPConnections := 0
	ctl, err := ControlPlane()
	if err != nil {
		if !errors.Is(err, ErrControlPlaneNotInit) {
			return nil, err
		}
	} else {
		activeTCPConnections = ctl.ActiveTCPConnections()
	}

	snapshot := control.SnapshotRuntimeStats(activeTCPConnections, control.DefaultUdpEndpointPool.Count(), windowSec, maxPoints)
	runtimeOverviewCache.Lock()
	runtimeOverviewCache.windowSec = windowSec
	runtimeOverviewCache.maxPoints = maxPoints
	runtimeOverviewCache.expiresAt = now.Add(runtimeOverviewCacheTTL)
	runtimeOverviewCache.snapshot = snapshot
	runtimeOverviewCache.Unlock()
	return &snapshot, nil
}
