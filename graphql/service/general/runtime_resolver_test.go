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

func TestRuntimeOverviewResolverRuntimeCapacityFields(t *testing.T) {
	resolver := newRuntimeOverviewResolver(&dae.RuntimeOverview{
		UDPSessions:           11,
		UDPTaskQueues:         12,
		UDPTaskDropTotal:      13,
		PacketSnifferSessions: 14,
	})

	if got := resolver.UdpSessions(); got != 11 {
		t.Fatalf("expected udp sessions 11, got %d", got)
	}
	if got := resolver.UdpTaskQueues(); got != 12 {
		t.Fatalf("expected udp task queues 12, got %d", got)
	}
	if got := resolver.UdpTaskDropTotal(); got != "13" {
		t.Fatalf("expected udp task drop total 13, got %s", got)
	}
	if got := resolver.PacketSnifferSessions(); got != 14 {
		t.Fatalf("expected packet sniffer sessions 14, got %d", got)
	}
}
