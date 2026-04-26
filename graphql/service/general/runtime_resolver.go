/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package general

import (
	"strconv"

	"github.com/daeuniverse/dae-wing/dae"
	"github.com/graph-gophers/graphql-go"
)

type RuntimeOverviewResolver struct {
	Overview        *dae.RuntimeOverview
	sampleResolvers []*RuntimeTrafficSampleResolver
}

type RuntimeTrafficSampleResolver struct {
	Sample *dae.RuntimeTrafficSample
}

func newRuntimeOverviewResolver(overview *dae.RuntimeOverview) *RuntimeOverviewResolver {
	resolver := &RuntimeOverviewResolver{Overview: overview}
	if overview == nil || len(overview.Samples) == 0 {
		return resolver
	}

	resolver.sampleResolvers = make([]*RuntimeTrafficSampleResolver, len(overview.Samples))
	for i := range overview.Samples {
		resolver.sampleResolvers[i] = &RuntimeTrafficSampleResolver{Sample: &overview.Samples[i]}
	}
	return resolver
}

func (r *Resolver) RuntimeOverview(args *struct {
	WindowSec int32
	MaxPoints int32
}) (*RuntimeOverviewResolver, error) {
	overview, err := dae.GetRuntimeOverview(int(args.WindowSec), int(args.MaxPoints))
	if err != nil {
		return nil, err
	}
	return newRuntimeOverviewResolver(overview), nil
}

func (r *RuntimeOverviewResolver) UpdatedAt() graphql.Time {
	return graphql.Time{Time: r.Overview.UpdatedAt}
}

func (r *RuntimeOverviewResolver) UploadRate() float64 {
	return float64(r.Overview.UploadRate)
}

func (r *RuntimeOverviewResolver) DownloadRate() float64 {
	return float64(r.Overview.DownloadRate)
}

func (r *RuntimeOverviewResolver) UploadTotal() string {
	return strconv.FormatUint(r.Overview.UploadTotal, 10)
}

func (r *RuntimeOverviewResolver) DownloadTotal() string {
	return strconv.FormatUint(r.Overview.DownloadTotal, 10)
}

func (r *RuntimeOverviewResolver) ActiveConnections() int32 {
	return int32(r.Overview.ActiveConnections)
}

func (r *RuntimeOverviewResolver) UdpSessions() int32 {
	return int32(r.Overview.UDPSessions)
}

func (r *RuntimeOverviewResolver) UdpTaskQueues() int32 {
	return int32(r.Overview.UDPTaskQueues)
}

func (r *RuntimeOverviewResolver) UdpTaskDropTotal() string {
	return strconv.FormatUint(r.Overview.UDPTaskDropTotal, 10)
}

func (r *RuntimeOverviewResolver) PacketSnifferSessions() int32 {
	return int32(r.Overview.PacketSnifferSessions)
}

func (r *RuntimeOverviewResolver) RssBytes() string {
	return strconv.FormatUint(r.Overview.RSSBytes, 10)
}

func (r *RuntimeOverviewResolver) HeapAllocBytes() string {
	return strconv.FormatUint(r.Overview.HeapAllocBytes, 10)
}

func (r *RuntimeOverviewResolver) Goroutines() int32 {
	return int32(r.Overview.Goroutines)
}

func (r *RuntimeOverviewResolver) Samples() []*RuntimeTrafficSampleResolver {
	return r.sampleResolvers
}

func (r *RuntimeTrafficSampleResolver) Timestamp() graphql.Time {
	return graphql.Time{Time: r.Sample.Timestamp}
}

func (r *RuntimeTrafficSampleResolver) UploadRate() float64 {
	return float64(r.Sample.UploadRate)
}

func (r *RuntimeTrafficSampleResolver) DownloadRate() float64 {
	return float64(r.Sample.DownloadRate)
}
