/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"net/http"

	"github.com/daeuniverse/dae-wing/orchestrator"
)

type runtimeStateResource struct {
	Running  bool   `json:"running"`
	Modified bool   `json:"modified"`
	Version  string `json:"version"`
}

type interfaceResource struct {
	Name          string                 `json:"name"`
	Index         int32                  `json:"index"`
	Up            bool                   `json:"up"`
	Addresses     []string               `json:"addresses"`
	DefaultRoutes []defaultRouteResource `json:"defaultRoutes,omitempty"`
}

type defaultRouteResource struct {
	IPVersion string  `json:"ipVersion"`
	Gateway   *string `json:"gateway,omitempty"`
	Source    *string `json:"source,omitempty"`
}

type interfaceFlagResource struct {
	Up      bool                   `json:"up"`
	Default []defaultRouteResource `json:"default,omitempty"`
}

func handleGeneralState(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	state, err := orchestrator.GetRuntimeState(r.Context())
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, runtimeStateResource{
		Running:  state.Running,
		Modified: state.Modified,
		Version:  state.Version,
	})
}

func handleGeneralInterfaces(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	up, hasUp := parseOptionalBool(r.URL.Query().Get("up"))
	onlyGlobalScope, _ := parseOptionalBool(r.URL.Query().Get("onlyGlobalScope"))
	var upPtr *bool
	if hasUp {
		upPtr = &up
	}
	interfaces, err := orchestrator.ListInterfaces(upPtr, onlyGlobalScope)
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]interfaceResource, 0, len(interfaces))
	for _, iface := range interfaces {
		routes := make([]defaultRouteResource, 0, len(iface.DefaultRoutes))
		for _, route := range iface.DefaultRoutes {
			routes = append(routes, defaultRouteResource(route))
		}
		items = append(items, interfaceResource{
			Name:          iface.Name,
			Index:         iface.Index,
			Up:            iface.Up,
			Addresses:     iface.Addresses,
			DefaultRoutes: routes,
		})
	}
	writeJSON(rw, http.StatusOK, map[string]any{"items": items})
}
