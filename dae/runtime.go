/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package dae

import (
	"errors"

	"github.com/daeuniverse/dae/control"
)

type RuntimeTrafficSample = control.RuntimeTrafficSample

type RuntimeOverview = control.RuntimeStatsSnapshot

func GetRuntimeOverview(windowSec int, maxPoints int) (*RuntimeOverview, error) {
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
	return &snapshot, nil
}
