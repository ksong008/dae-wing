/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type RuntimeState struct {
	Running  bool
	Modified bool
	Version  string
}

type InterfaceInfo struct {
	Name          string
	Index         int32
	Up            bool
	Addresses     []string
	DefaultRoutes []DefaultRouteInfo
}

type DefaultRouteInfo struct {
	IPVersion string  `json:"ipVersion"`
	Gateway   *string `json:"gateway,omitempty"`
	Source    *string `json:"source,omitempty"`
}

func GetRuntimeState(ctx context.Context) (*RuntimeState, error) {
	running, err := runtimeRunning(ctx)
	if err != nil {
		return nil, err
	}
	modified, err := runtimeModified(ctx)
	if err != nil {
		return nil, err
	}
	return &RuntimeState{
		Running:  running,
		Modified: modified,
		Version:  db.AppVersion,
	}, nil
}

func ListInterfaces(up *bool, onlyGlobalScope bool) ([]InterfaceInfo, error) {
	linkList, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	infos := make([]InterfaceInfo, 0, len(linkList))
	for _, link := range linkList {
		if up != nil {
			linkUp := link.Attrs().Flags&unix.RTF_UP == unix.RTF_UP
			if linkUp != *up {
				continue
			}
		}
		addresses, err := interfaceAddresses(link, onlyGlobalScope)
		if err != nil {
			return nil, err
		}
		routes, err := interfaceDefaultRoutes(link)
		if err != nil {
			return nil, err
		}
		infos = append(infos, InterfaceInfo{
			Name:          link.Attrs().Name,
			Index:         int32(link.Attrs().Index),
			Up:            link.Attrs().Flags&unix.RTF_UP == unix.RTF_UP,
			Addresses:     addresses,
			DefaultRoutes: routes,
		})
	}
	return infos, nil
}

func runtimeRunning(ctx context.Context) (bool, error) {
	var model db.System
	q := db.DB(ctx).Select("running").Model(&db.System{}).FirstOrCreate(&model)
	if q.Error != nil {
		return false, q.Error
	}
	return model.Running, nil
}

func runtimeModified(ctx context.Context) (bool, error) {
	var model db.System
	tx := db.BeginReadOnlyTx(ctx)
	defer tx.Commit()

	q := tx.Model(&model).Preload("RunningGroups").FirstOrCreate(&model)
	if q.Error != nil {
		return false, q.Error
	}
	if !model.Running {
		return false, nil
	}

	var selectedConfig db.Config
	if q = tx.Model(&db.Config{}).Where("selected = ?", true).First(&selectedConfig); q.Error != nil || q.RowsAffected == 0 {
		return true, q.Error
	}
	var selectedDNS db.Dns
	if q = tx.Model(&db.Dns{}).Where("selected = ?", true).First(&selectedDNS); q.Error != nil || q.RowsAffected == 0 {
		return true, q.Error
	}
	var selectedRouting db.Routing
	if q = tx.Model(&db.Routing{}).Where("selected = ?", true).First(&selectedRouting); q.Error != nil || q.RowsAffected == 0 {
		return true, q.Error
	}

	if selectedConfig.ID != *model.RunningConfigID || selectedConfig.Version != model.RunningConfigVersion ||
		selectedDNS.ID != *model.RunningDnsID || selectedDNS.Version != model.RunningDnsVersion ||
		selectedRouting.ID != *model.RunningRoutingID || selectedRouting.Version != model.RunningRoutingVersion ||
		len(model.RunningGroups) == 0 {
		return true, nil
	}

	var versionSum uint
	var ids []string
	for _, group := range model.RunningGroups {
		versionSum += group.Version
		ids = append(ids, fmt.Sprintf("%x", group.ID))
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if versionSum != model.RunningGroupVersionSum || strings.Join(ids, ",") != model.RunningGroupIds {
		return true, nil
	}
	return false, nil
}

func interfaceAddresses(link netlink.Link, onlyGlobalScope bool) ([]string, error) {
	var addresses []string
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		list, err := netlink.AddrList(link, family)
		if err != nil {
			return nil, err
		}
		for _, addr := range list {
			if onlyGlobalScope && addr.Scope != unix.RT_SCOPE_UNIVERSE {
				continue
			}
			addresses = append(addresses, addr.IPNet.String())
		}
	}
	return addresses, nil
}

func interfaceDefaultRoutes(link netlink.Link) ([]DefaultRouteInfo, error) {
	var routes []DefaultRouteInfo
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		list, err := netlink.RouteList(link, family)
		if err != nil {
			return nil, err
		}
		for _, route := range list {
			if route.Dst != nil {
				continue
			}
			info := DefaultRouteInfo{}
			if family == unix.AF_INET {
				info.IPVersion = "4"
			} else {
				info.IPVersion = "6"
			}
			if route.Gw != nil {
				gateway := route.Gw.String()
				info.Gateway = &gateway
			}
			if route.Src != nil {
				source := route.Src.String()
				info.Source = &source
			}
			routes = append(routes, info)
		}
	}
	return routes, nil
}
