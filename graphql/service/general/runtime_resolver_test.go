/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package general

import (
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/dae"
)

func TestNewRuntimeOverviewResolverCachesSampleResolvers(t *testing.T) {
	now := time.Unix(1711111111, 0)
	overview := &dae.RuntimeOverview{
		Samples: []dae.RuntimeTrafficSample{
			{Timestamp: now, UploadRate: 1, DownloadRate: 2},
			{Timestamp: now.Add(time.Second), UploadRate: 3, DownloadRate: 4},
		},
	}

	resolver := newRuntimeOverviewResolver(overview)
	samples := resolver.Samples()

	if len(samples) != len(overview.Samples) {
		t.Fatalf("expected %d sample resolvers, got %d", len(overview.Samples), len(samples))
	}
	if samples[0] == nil || samples[1] == nil {
		t.Fatal("expected sample resolvers to be initialized")
	}
	if samples[0].Sample != &overview.Samples[0] {
		t.Fatal("expected first sample resolver to reference overview sample directly")
	}
	if samples[1].Sample != &overview.Samples[1] {
		t.Fatal("expected second sample resolver to reference overview sample directly")
	}
	if got := samples[1].DownloadRate(); got != 4 {
		t.Fatalf("unexpected download rate: %v", got)
	}
}

func TestNewRuntimeOverviewResolverHandlesEmptyOverview(t *testing.T) {
	resolver := newRuntimeOverviewResolver(&dae.RuntimeOverview{})
	if len(resolver.Samples()) != 0 {
		t.Fatalf("expected no sample resolvers, got %d", len(resolver.Samples()))
	}
}
