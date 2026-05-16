/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"net/http"
	"reflect"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	daeConfig "github.com/daeuniverse/dae/config"
)

func handleOpenAPI(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	writeJSON(rw, http.StatusOK, OpenAPIDocument())
}

func OpenAPIDocument() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "dae-wing HTTP API",
			"version":     db.AppVersion,
			"description": "Minimal REST/OpenAPI + SSE control-plane surface for dae-wing.",
		},
		"paths":      openAPIPaths(),
		"components": map[string]any{"schemas": openAPISchemas()},
	}
}

func openAPIPaths() map[string]any {
	return map[string]any{
		"/api/health": map[string]any{
			"get": map[string]any{
				"summary":     "Health check",
				"description": "Returns a minimal process health signal.",
				"responses":   map[string]any{"200": jsonResponse("Health check.", "HealthResponse")},
			},
		},
		"/api/auth/status": map[string]any{
			"get": map[string]any{
				"summary":     "Read auth bootstrap status",
				"description": "Returns the current user count for first-user bootstrap flows.",
				"responses":   map[string]any{"200": jsonResponse("Auth status.", "AuthStatusResponse")},
			},
		},
		"/api/auth/token": map[string]any{
			"post": map[string]any{
				"summary":     "Issue bearer token",
				"description": "Authenticates a user and returns a JWT bearer token.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("TokenRequest")}},
				},
				"responses": map[string]any{"200": jsonResponse("Token issued.", "TokenResponse")},
			},
		},
		"/api/auth/users": map[string]any{
			"post": map[string]any{
				"summary":     "Create first user",
				"description": "Creates the initial local user if none exists and returns a token.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("CreateUserRequest")}},
				},
				"responses": map[string]any{"201": jsonResponse("User created.", "TokenResponse")},
			},
		},
		"/api/configs":        resourceCollectionPath("configs", "config", "ConfigResource", "ConfigList", "ConfigCreateRequest"),
		"/api/configs/parsed": configPreviewPath(),
		"/api/configs/flat-desc": map[string]any{
			"get": map[string]any{
				"summary":     "List config flat descriptors",
				"description": "Returns the flattened config field descriptors used by config editors.",
				"responses":   map[string]any{"200": jsonResponse("Config flat descriptor list.", "ConfigFlatDescList")},
			},
		},
		"/api/configs/{id}":              resourceItemPath("config", "ConfigResource", "ConfigUpdateRequest"),
		"/api/configs/{id}/select":       resourceSelectPath("config"),
		"/api/dns":                       resourceCollectionPath("dns resources", "dns resource", "DNSResource", "DNSList", "DNSCreateRequest"),
		"/api/dns/parsed":                parsedSectionPath("dns preview", "ParsedDNSResponse"),
		"/api/dns/{id}":                  resourceItemPath("dns resource", "DNSResource", "DNSUpdateRequest"),
		"/api/dns/{id}/select":           resourceSelectPath("dns resource"),
		"/api/general/interfaces":        generalInterfacesPath(),
		"/api/general/state":             generalStatePath(),
		"/api/general/cache-stats":       generalCacheStatsPath(),
		"/api/groups":                    groupCollectionPath(),
		"/api/groups/{id}":               groupItemPath(),
		"/api/groups/{id}/nodes":         groupNodesPath(),
		"/api/groups/{id}/subscriptions": groupSubscriptionsPath(),
		"/api/nodes":                     nodeCollectionPath(),
		"/api/nodes/{id}":                nodeItemPath(),
		"/api/nodes/latencies":           nodeLatenciesPath(),
		"/api/routings":                  resourceCollectionPath("routings", "routing", "RoutingResource", "RoutingList", "RoutingCreateRequest"),
		"/api/routings/parsed":           parsedSectionPath("routing preview", "ParsedRoutingResponse"),
		"/api/routings/{id}":             resourceItemPath("routing", "RoutingResource", "RoutingUpdateRequest"),
		"/api/routings/{id}/select":      resourceSelectPath("routing"),
		"/api/subscriptions":             subscriptionCollectionPath(),
		"/api/subscriptions/{id}":        subscriptionItemPath(),
		"/api/subscriptions/{id}/nodes":  subscriptionNodesPath(),
		"/api/subscriptions/{id}/refresh": map[string]any{
			"post": map[string]any{
				"summary":     "Refresh subscription",
				"description": "Re-fetches a subscription and re-imports its nodes.",
				"parameters":  []map[string]any{idPathParameter()},
				"responses": map[string]any{
					"200": jsonResponse("Subscription refreshed.", "SubscriptionResource"),
				},
			},
		},
		"/api/user/me": map[string]any{
			"get": map[string]any{
				"summary":     "Get current user",
				"description": "Returns the authenticated user profile.",
				"responses":   map[string]any{"200": jsonResponse("User profile.", "UserResource")},
			},
			"patch": map[string]any{
				"summary":     "Update current user",
				"description": "Updates username, name, and avatar for the authenticated user.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("UserPatchRequest")}},
				},
				"responses": map[string]any{"200": jsonResponse("User updated.", "UserResource")},
			},
		},
		"/api/user/me/dae-bundle": map[string]any{
			"get": map[string]any{
				"summary":     "Export dae control-plane bundle",
				"description": "Exports configs, dns, routings, groups, subscriptions, nodes, and default selection metadata as a single JSON bundle.",
				"responses":   map[string]any{"200": jsonResponse("dae bundle exported.", "DAEBundle")},
			},
			"put": map[string]any{
				"summary":     "Import dae control-plane bundle",
				"description": "Replaces the stored control-plane resources with one JSON bundle and restores selected/default resources plus mode.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("DAEBundle")}},
				},
				"responses": map[string]any{"200": jsonResponse("dae bundle imported.", "BooleanResult")},
			},
		},
		"/api/user/me/dae-config-file": map[string]any{
			"get": map[string]any{
				"summary":     "Export dae config file",
				"description": "Exports the currently selected config, dns, routing, and referenced group resources as a native dae configuration file.",
				"responses":   map[string]any{"200": jsonResponse("dae config file exported.", "DAEConfigFileResponse")},
			},
			"put": map[string]any{
				"summary":     "Import dae config file",
				"description": "Parses a native dae configuration file and replaces the current control-plane resources with the imported model.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("DAEConfigFileImportRequest")}},
				},
				"responses": map[string]any{"200": jsonResponse("dae config file imported.", "DAEConfigFileImportResponse")},
			},
		},
		"/api/user/me/dae-config-file/preview": map[string]any{
			"post": map[string]any{
				"summary":     "Preview dae config file import",
				"description": "Parses a native dae configuration file and returns the projected control-plane snapshot plus import warnings without changing stored resources.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("DAEConfigFileImportRequest")}},
				},
				"responses": map[string]any{"200": jsonResponse("dae config file preview.", "DAEConfigFilePreviewResponse")},
			},
		},
		"/api/user/me/default-resources": map[string]any{
			"post": map[string]any{
				"summary":     "Ensure current user default resources",
				"description": "Ensures default config, routing, dns, group, and mode values for the authenticated user and persists them into JSON storage.",
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type":     "object",
								"required": []string{"configName", "global", "dnsName", "dns", "routingName", "routing", "groupName", "policy", "policyParams", "mode"},
								"properties": map[string]any{
									"configName":  map[string]any{"type": "string"},
									"global":      parsedGlobalSchema(),
									"dnsName":     map[string]any{"type": "string"},
									"dns":         map[string]any{"type": "string"},
									"routingName": map[string]any{"type": "string"},
									"routing":     map[string]any{"type": "string"},
									"groupName":   map[string]any{"type": "string"},
									"policy":      map[string]any{"type": "string"},
									"policyParams": map[string]any{
										"type":  "array",
										"items": schemaRef("ParamResource"),
									},
									"mode": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
				"responses": map[string]any{"200": jsonResponse("Default resources ensured.", "EnsureDefaultResourcesResponse")},
			},
		},
		"/api/user/me/password": map[string]any{
			"post": map[string]any{
				"summary":     "Change current user password",
				"description": "Updates the current user password and returns a fresh JWT.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("PasswordChangeRequest")}},
				},
				"responses": map[string]any{"200": jsonResponse("Password updated.", "TokenResponse")},
			},
		},
		"/api/user/me/storage": map[string]any{
			"get": map[string]any{
				"summary":     "Read current user JSON storage",
				"description": "Reads either the full JSON storage blob or selected `path` query values.",
				"parameters": []map[string]any{
					{"name": "path", "in": "query", "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{"200": jsonResponse("Storage values.", "JSONStorageValuesResponse")},
			},
			"put": map[string]any{
				"summary":     "Set current user JSON storage",
				"description": "Sets JSON storage values using aligned `paths` and `values` arrays.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("JSONStorageSetRequest")}},
				},
				"responses": map[string]any{"200": jsonResponse("Storage updated.", "CountResponse")},
			},
			"delete": map[string]any{
				"summary":     "Remove current user JSON storage values",
				"description": "Removes selected JSON storage paths, or clears all storage when no paths are provided.",
				"requestBody": map[string]any{
					"required": false,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("JSONStorageRemoveRequest")}},
				},
				"responses": map[string]any{"200": jsonResponse("Storage removed.", "CountResponse")},
			},
		},
		"/api/runtime/overview": map[string]any{
			"get": map[string]any{
				"summary":     "Read runtime overview",
				"description": "Returns current upload/download rates, totals, and sampled traffic points.",
				"parameters":  runtimeOverviewQueryParameters(),
				"responses": map[string]any{
					"200": jsonResponse("Runtime overview.", "RuntimeOverview"),
				},
			},
		},
		"/api/runtime/log-level": map[string]any{
			"get": map[string]any{
				"summary":     "Read runtime log level",
				"description": "Returns the current in-process log level.",
				"responses":   map[string]any{"200": jsonResponse("Runtime log level.", "RuntimeLogLevel")},
			},
			"patch": map[string]any{
				"summary":     "Set runtime log level",
				"description": "Applies a new in-process log level immediately without reloading dae.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("RuntimeLogLevel")}},
				},
				"responses": map[string]any{"200": jsonResponse("Runtime log level updated.", "RuntimeLogLevel")},
			},
		},
		"/api/runtime/reload": map[string]any{
			"post": map[string]any{
				"summary":     "Reload selected runtime config",
				"description": "Builds the selected config, dns, routing, groups, and nodes, then reloads dae.",
				"requestBody": map[string]any{
					"required": false,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"dry":        map[string]any{"type": "boolean", "default": false},
									"timeoutSec": map[string]any{"type": "integer", "default": 30, "maximum": 120},
								},
							},
						},
					},
				},
				"responses": map[string]any{
					"200": jsonResponse("Reload applied.", "ReloadResponse"),
				},
			},
		},
		"/api/runtime/stop": map[string]any{
			"post": map[string]any{
				"summary":     "Stop runtime",
				"description": "Stops dae and marks persisted running state as false.",
				"requestBody": map[string]any{
					"required": false,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"timeoutSec": map[string]any{"type": "integer", "default": 10, "maximum": 120},
								},
							},
						},
					},
				},
				"responses": map[string]any{
					"200": jsonResponse("Runtime stopped.", "StopResponse"),
				},
			},
		},
		"/api/logs": map[string]any{
			"get": map[string]any{
				"summary":     "Query runtime logs",
				"description": "Reads the bounded on-disk JSONL runtime log cache.",
				"parameters":  logQueryParameters(),
				"responses":   map[string]any{"200": jsonResponse("Runtime logs.", "LogList")},
			},
			"delete": map[string]any{
				"summary":     "Clear runtime logs",
				"description": "Clears the on-disk runtime log cache.",
				"responses":   map[string]any{"200": jsonResponse("Runtime logs cleared.", "LogClearResponse")},
			},
		},
		"/api/logs/settings": map[string]any{
			"get": map[string]any{
				"summary":     "Read runtime log cache settings",
				"description": "Returns the current log cache entry and file-size limits.",
				"responses":   map[string]any{"200": jsonResponse("Runtime log cache settings.", "LogSettings")},
			},
			"patch": map[string]any{
				"summary":     "Update runtime log cache settings",
				"description": "Updates log cache limits and prunes the on-disk cache immediately.",
				"requestBody": map[string]any{
					"required": true,
					"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("LogSettingsPatch")}},
				},
				"responses": map[string]any{"200": jsonResponse("Runtime log cache settings updated.", "LogSettings")},
			},
		},
		"/api/events/runtime": map[string]any{
			"get": map[string]any{
				"summary":     "Stream runtime events",
				"description": "Streams an initial full runtime overview event, follow-up delta overview events, and runtime error events using Server-Sent Events.",
				"parameters":  runtimeEventsQueryParameters(),
				"responses": map[string]any{
					"200": map[string]any{
						"description": "SSE stream.",
						"content": map[string]any{
							"text/event-stream": map[string]any{
								"schema": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		"/api/events/logs": map[string]any{
			"get": map[string]any{
				"summary":     "Stream runtime log events",
				"description": "Streams new runtime log entries using Server-Sent Events.",
				"parameters":  logEventsQueryParameters(),
				"responses": map[string]any{
					"200": map[string]any{
						"description": "SSE stream.",
						"content": map[string]any{
							"text/event-stream": map[string]any{
								"schema": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}
}

func logEventsQueryParameters() []map[string]any {
	parameters := append([]map[string]any{}, logQueryParameters()[:2]...)
	parameters = append(parameters, map[string]any{
		"name":        "access_token",
		"in":          "query",
		"description": "Optional bearer token fallback for browser EventSource clients that cannot attach Authorization headers.",
		"schema":      map[string]any{"type": "string"},
	})
	return parameters
}

func logQueryParameters() []map[string]any {
	return []map[string]any{
		{
			"name":        "level",
			"in":          "query",
			"description": "Optional log level filter. Use `all` or omit for all levels.",
			"schema":      map[string]any{"type": "string"},
		},
		{
			"name":        "q",
			"in":          "query",
			"description": "Optional case-insensitive keyword filter over message and structured fields.",
			"schema":      map[string]any{"type": "string"},
		},
		{
			"name":        "limit",
			"in":          "query",
			"description": "Maximum number of matching entries to return.",
			"schema":      map[string]any{"type": "integer", "default": 500, "maximum": 2000},
		},
	}
}

func runtimeEventsQueryParameters() []map[string]any {
	parameters := append([]map[string]any{}, runtimeOverviewQueryParameters()...)
	parameters = append(parameters, map[string]any{
		"name":        "access_token",
		"in":          "query",
		"description": "Optional bearer token fallback for browser EventSource clients that cannot attach Authorization headers.",
		"schema":      map[string]any{"type": "string"},
	})
	return parameters
}

func runtimeOverviewQueryParameters() []map[string]any {
	return []map[string]any{
		{
			"name":        "windowSec",
			"in":          "query",
			"description": "Window size for sampled traffic data in seconds.",
			"schema":      map[string]any{"type": "integer", "default": defaultOverviewWindowSec, "maximum": maxOverviewWindowSec},
		},
		{
			"name":        "maxPoints",
			"in":          "query",
			"description": "Maximum number of sampled traffic points.",
			"schema":      map[string]any{"type": "integer", "default": defaultOverviewMaxPoints, "maximum": maxOverviewMaxPoints},
		},
	}
}

func openAPISchemas() map[string]any {
	return map[string]any{
		"RuntimeTrafficSample": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"timestamp":    map[string]any{"type": "string", "format": "date-time"},
				"uploadRate":   map[string]any{"type": "string"},
				"downloadRate": map[string]any{"type": "string"},
			},
		},
		"RuntimeOverview": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"updatedAt":             map[string]any{"type": "string", "format": "date-time"},
				"uploadRate":            map[string]any{"type": "string"},
				"downloadRate":          map[string]any{"type": "string"},
				"uploadTotal":           map[string]any{"type": "string"},
				"downloadTotal":         map[string]any{"type": "string"},
				"activeConnections":     map[string]any{"type": "integer"},
				"udpSessions":           map[string]any{"type": "integer"},
				"udpTaskQueues":         map[string]any{"type": "integer"},
				"udpTaskDropTotal":      map[string]any{"type": "string"},
				"packetSnifferSessions": map[string]any{"type": "integer"},
				"rssBytes":              map[string]any{"type": "string"},
				"heapAllocBytes":        map[string]any{"type": "string"},
				"goroutines":            map[string]any{"type": "integer"},
				"samples": map[string]any{
					"type":  "array",
					"items": schemaRef("RuntimeTrafficSample"),
				},
			},
		},
		"RuntimeLogLevel": map[string]any{
			"type":     "object",
			"required": []string{"level"},
			"properties": map[string]any{
				"level": map[string]any{"type": "string"},
			},
		},
		"LogEntry": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":      map[string]any{"type": "integer"},
				"ts":      map[string]any{"type": "string", "format": "date-time"},
				"level":   map[string]any{"type": "string"},
				"message": map[string]any{"type": "string"},
				"fields": map[string]any{
					"type":                 "object",
					"additionalProperties": map[string]any{"type": "string"},
				},
			},
		},
		"LogList": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{"type": "array", "items": schemaRef("LogEntry")},
			},
		},
		"LogSettings": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"maxEntries":    map[string]any{"type": "integer"},
				"maxBytes":      map[string]any{"type": "integer"},
				"minMaxEntries": map[string]any{"type": "integer"},
				"maxMaxEntries": map[string]any{"type": "integer"},
				"minMaxBytes":   map[string]any{"type": "integer"},
				"maxMaxBytes":   map[string]any{"type": "integer"},
			},
		},
		"LogSettingsPatch": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"maxEntries": map[string]any{"type": "integer"},
				"maxBytes":   map[string]any{"type": "integer"},
			},
		},
		"LogClearResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cleared": map[string]any{"type": "boolean"},
			},
		},
		"ReloadResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"applied": map[string]any{"type": "integer"},
				"dry":     map[string]any{"type": "boolean"},
			},
		},
		"SelectionResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"applied":    map[string]any{"type": "integer"},
				"selectedId": map[string]any{"type": "integer"},
			},
		},
		"StopResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"stopped": map[string]any{"type": "boolean"},
			},
		},
		"ErrorResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"error": map[string]any{"type": "string"},
			},
		},
		"HealthResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"healthCheck": map[string]any{"type": "integer"},
			},
		},
		"AuthStatusResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"numberUsers": map[string]any{"type": "integer"},
			},
		},
		"TokenRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"username": map[string]any{"type": "string"},
				"password": map[string]any{"type": "string"},
			},
		},
		"CreateUserRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"username": map[string]any{"type": "string"},
				"password": map[string]any{"type": "string"},
			},
		},
		"TokenResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"token": map[string]any{"type": "string"},
			},
		},
		"CountResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"updated": map[string]any{"type": "integer"},
				"removed": map[string]any{"type": "integer"},
			},
		},
		"EnsureDefaultResourcesResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"defaultConfigID":  map[string]any{"type": "string"},
				"defaultRoutingID": map[string]any{"type": "string"},
				"defaultDNSID":     map[string]any{"type": "string"},
				"defaultGroupID":   map[string]any{"type": "string"},
				"mode":             map[string]any{"type": "string"},
			},
		},
		"ParamResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{"type": "string"},
				"val": map[string]any{"type": "string"},
			},
		},
		"BatchIDsRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			},
		},
		"ConfigResource": configResourceSchema(),
		"ConfigList":     listSchema("ConfigResource"),
		"ParsedConfigResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"global":       map[string]any{"type": "string"},
				"parsedGlobal": parsedGlobalSchema(),
			},
		},
		"ConfigFlatDesc": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":         map[string]any{"type": "string"},
				"mapping":      map[string]any{"type": "string"},
				"isArray":      map[string]any{"type": "boolean"},
				"defaultValue": map[string]any{"type": "string"},
				"required":     map[string]any{"type": "boolean"},
				"type":         map[string]any{"type": "string"},
				"desc":         map[string]any{"type": "string"},
			},
		},
		"ConfigFlatDescList": map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": schemaRef("ConfigFlatDesc")}}},
		"DNSResource":        dnsResourceSchema(),
		"DNSList":            listSchema("DNSResource"),
		"ParsedSectionRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"raw": map[string]any{"type": "string"},
			},
		},
		"ParsedParam": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":          map[string]any{"type": "string"},
				"val":          map[string]any{"type": "string"},
				"andFunctions": map[string]any{"type": "array", "items": schemaRef("ParsedFunction")},
			},
		},
		"ParsedFunction": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"not":    map[string]any{"type": "boolean"},
				"params": map[string]any{"type": "array", "items": schemaRef("ParsedParam")},
			},
		},
		"ParsedFunctionOrPlaintext": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":      map[string]any{"type": "string"},
				"plaintext": map[string]any{"type": "string"},
				"function":  schemaRef("ParsedFunction"),
				"functions": map[string]any{"type": "array", "items": schemaRef("ParsedFunction")},
			},
		},
		"ParsedRoutingRule": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"conditions": map[string]any{"type": "array", "items": schemaRef("ParsedFunction")},
				"outbound":   schemaRef("ParsedFunction"),
			},
		},
		"ParsedRoutingResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"string":   map[string]any{"type": "string"},
				"rules":    map[string]any{"type": "array", "items": schemaRef("ParsedRoutingRule")},
				"fallback": schemaRef("ParsedFunctionOrPlaintext"),
			},
		},
		"ParsedDNSRoutingResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request":  schemaRef("ParsedRoutingResponse"),
				"response": schemaRef("ParsedRoutingResponse"),
			},
		},
		"ParsedDNSResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"string":   map[string]any{"type": "string"},
				"upstream": map[string]any{"type": "array", "items": schemaRef("ParsedParam")},
				"routing":  schemaRef("ParsedDNSRoutingResponse"),
			},
		},
		"GroupResource": groupSchema(),
		"GroupList":     map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": schemaRef("GroupResource")}}},
		"GroupSubscriptionResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subscriptionId":  map[string]any{"type": "integer"},
				"nameFilterRegex": map[string]any{"type": "string"},
				"matchedCount":    map[string]any{"type": "integer"},
				"matchedNodes":    map[string]any{"type": "array", "items": schemaRef("NodeResource")},
				"updatedAt":       map[string]any{"type": "string", "format": "date-time"},
				"status":          map[string]any{"type": "string"},
				"info":            map[string]any{"type": "string"},
				"link":            map[string]any{"type": "string"},
				"tag":             map[string]any{"type": "string"},
			},
		},
		"GroupCreateRequest":        groupCreateRequestSchema(),
		"GroupUpdateRequest":        groupUpdateRequestSchema(),
		"GroupNodesRequest":         groupNodesRequestSchema(),
		"GroupSubscriptionsRequest": groupSubscriptionsRequestSchema(),
		"InterfaceResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":          map[string]any{"type": "string"},
				"index":         map[string]any{"type": "integer"},
				"up":            map[string]any{"type": "boolean"},
				"addresses":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"defaultRoutes": map[string]any{"type": "array", "items": schemaRef("DefaultRouteResource")},
			},
		},
		"InterfaceList": map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": schemaRef("InterfaceResource")}}},
		"DefaultRouteResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ipVersion": map[string]any{"type": "string"},
				"gateway":   map[string]any{"type": "string"},
				"source":    map[string]any{"type": "string"},
			},
		},
		"RuntimeStateResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"running":  map[string]any{"type": "boolean"},
				"modified": map[string]any{"type": "boolean"},
				"version":  map[string]any{"type": "string"},
			},
		},
		"CacheStatsResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"realDomainCacheEntries":   map[string]any{"type": "integer"},
				"dnsCacheEntries":          map[string]any{"type": "integer"},
				"dnsForwarderCacheEntries": map[string]any{"type": "integer"},
				"udpEndpointPoolEntries":   map[string]any{"type": "integer"},
				"anyfromPoolEntries":       map[string]any{"type": "integer"},
				"packetSnifferEntries":     map[string]any{"type": "integer"},
				"udpTaskQueueEntries":      map[string]any{"type": "integer"},
				"udpTaskDropTotal":         map[string]any{"type": "integer"},
				"activeTCPConnections":     map[string]any{"type": "integer"},
				"nodeLatencyCacheEntries":  map[string]any{"type": "integer"},
			},
		},
		"NodeResource":              nodeSchema(),
		"NodeList":                  nodeListSchema(),
		"NodeImportRequest":         nodeImportRequestSchema(),
		"NodeImportResult":          nodeImportResultSchema(),
		"NodeImportResultList":      map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": schemaRef("NodeImportResult")}}},
		"NodeUpdateRequest":         nodeUpdateRequestSchema(),
		"NodeLatencyResource":       nodeLatencySchema(),
		"NodeLatencyList":           map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": schemaRef("NodeLatencyResource")}}},
		"RoutingResource":           routingResourceSchema(),
		"RoutingList":               listSchema("RoutingResource"),
		"SubscriptionResource":      subscriptionSchema(),
		"SubscriptionList":          map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": schemaRef("SubscriptionResource")}}},
		"SubscriptionCreateRequest": subscriptionCreateRequestSchema(),
		"SubscriptionUpdateRequest": subscriptionUpdateRequestSchema(),
		"SubscriptionImportResult":  subscriptionImportResultSchema(),
		"UserResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"username": map[string]any{"type": "string"},
				"name":     map[string]any{"type": "string"},
				"avatar":   map[string]any{"type": "string"},
			},
		},
		"UserPatchRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"username":    map[string]any{"type": "string"},
				"name":        map[string]any{"type": "string"},
				"avatar":      map[string]any{"type": "string"},
				"clearName":   map[string]any{"type": "boolean"},
				"clearAvatar": map[string]any{"type": "boolean"},
			},
		},
		"PasswordChangeRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"currentPassword": map[string]any{"type": "string"},
				"newPassword":     map[string]any{"type": "string"},
			},
		},
		"JSONStorageSetRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"values": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"JSONStorageRemoveRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"JSONStorageValuesResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"values": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"BooleanResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"imported": map[string]any{"type": "boolean"},
			},
		},
		"DAEConfigFileResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string"},
				"warnings": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"DAEConfigFileImportRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename":   map[string]any{"type": "string"},
				"namePrefix": map[string]any{"type": "string"},
				"content":    map[string]any{"type": "string"},
			},
			"required": []string{"content"},
		},
		"DAEConfigFileImportResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"imported": map[string]any{"type": "boolean"},
				"warnings": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"DAEConfigFilePreviewResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"bundle":   schemaRef("DAEBundle"),
				"warnings": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"DAEBundleDefaults": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"configId":  map[string]any{"type": "integer"},
				"dnsId":     map[string]any{"type": "integer"},
				"routingId": map[string]any{"type": "integer"},
				"groupId":   map[string]any{"type": "integer"},
			},
		},
		"DAEBundleSelected": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"configId":  map[string]any{"type": "integer"},
				"dnsId":     map[string]any{"type": "integer"},
				"routingId": map[string]any{"type": "integer"},
			},
		},
		"DAEBundleConfig": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "integer"},
				"name":   map[string]any{"type": "string"},
				"global": map[string]any{"type": "string"},
			},
		},
		"DAEBundleDNS": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "integer"},
				"name": map[string]any{"type": "string"},
				"dns":  map[string]any{"type": "string"},
			},
		},
		"DAEBundleRouting": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":      map[string]any{"type": "integer"},
				"name":    map[string]any{"type": "string"},
				"routing": map[string]any{"type": "string"},
			},
		},
		"DAEBundleSubscription": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":         map[string]any{"type": "integer"},
				"updatedAt":  map[string]any{"type": "string", "format": "date-time"},
				"link":       map[string]any{"type": "string"},
				"cronExp":    map[string]any{"type": "string"},
				"cronEnable": map[string]any{"type": "boolean"},
				"status":     map[string]any{"type": "string"},
				"info":       map[string]any{"type": "string"},
				"tag":        map[string]any{"type": "string"},
			},
		},
		"DAEBundleNode": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":             map[string]any{"type": "integer"},
				"link":           map[string]any{"type": "string"},
				"name":           map[string]any{"type": "string"},
				"address":        map[string]any{"type": "string"},
				"protocol":       map[string]any{"type": "string"},
				"tag":            map[string]any{"type": "string"},
				"subscriptionId": map[string]any{"type": "integer"},
			},
		},
		"DAEBundleGroupSubscription": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subscriptionId":  map[string]any{"type": "integer"},
				"nameFilterRegex": map[string]any{"type": "string"},
			},
		},
		"DAEBundleGroup": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":                   map[string]any{"type": "integer"},
				"name":                 map[string]any{"type": "string"},
				"policy":               map[string]any{"type": "string"},
				"policyParams":         map[string]any{"type": "array", "items": schemaRef("ParamResource")},
				"nodeIds":              map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
				"subscriptionBindings": map[string]any{"type": "array", "items": schemaRef("DAEBundleGroupSubscription")},
			},
		},
		"DAEBundle": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"schemaVersion": map[string]any{"type": "integer"},
				"exportedAt":    map[string]any{"type": "string", "format": "date-time"},
				"mode":          map[string]any{"type": "string"},
				"defaults":      schemaRef("DAEBundleDefaults"),
				"selected":      schemaRef("DAEBundleSelected"),
				"configs":       map[string]any{"type": "array", "items": schemaRef("DAEBundleConfig")},
				"dnss":          map[string]any{"type": "array", "items": schemaRef("DAEBundleDNS")},
				"routings":      map[string]any{"type": "array", "items": schemaRef("DAEBundleRouting")},
				"subscriptions": map[string]any{"type": "array", "items": schemaRef("DAEBundleSubscription")},
				"nodes":         map[string]any{"type": "array", "items": schemaRef("DAEBundleNode")},
				"groups":        map[string]any{"type": "array", "items": schemaRef("DAEBundleGroup")},
			},
		},
		"ConfigCreateRequest":  configMutationSchema(),
		"ConfigUpdateRequest":  configMutationSchema(),
		"DNSCreateRequest":     resourceMutationSchema("dns"),
		"DNSUpdateRequest":     resourceMutationSchema("dns"),
		"RoutingCreateRequest": resourceMutationSchema("routing"),
		"RoutingUpdateRequest": resourceMutationSchema("routing"),
	}
}

func resourceCollectionPath(collectionName string, singularName string, resourceSchemaName string, listSchemaName string, createSchemaName string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List " + collectionName,
			"description": "Returns stored " + collectionName + ". Optional `id`, `selected`, and `expand=parsed` queries adjust the result shape.",
			"parameters": []map[string]any{
				{
					"name":        "id",
					"in":          "query",
					"description": "Optional id filter.",
					"schema":      map[string]any{"type": "integer"},
				},
				{
					"name":        "selected",
					"in":          "query",
					"description": "Optional selected-state filter.",
					"schema":      map[string]any{"type": "boolean"},
				},
				{
					"name":        "expand",
					"in":          "query",
					"description": "Optional expansion list. Use `parsed` to include structured parsed views.",
					"schema":      map[string]any{"type": "string"},
				},
			},
			"responses": map[string]any{
				"200": jsonResponse("Resource list.", listSchemaName),
			},
		},
		"post": map[string]any{
			"summary":     "Create " + singularName,
			"description": "Creates a new " + singularName + " using the stored raw section format.",
			"requestBody": map[string]any{
				"required": false,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": schemaRef(createSchemaName),
					},
				},
			},
			"responses": map[string]any{
				"201": jsonResponse("Resource created.", resourceSchemaName),
			},
		},
	}
}

func configPreviewPath() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Preview config global section",
			"description": "Parses raw `global` DSL or marshals `parsedGlobal` input and returns both normalized raw text and structured fields.",
			"requestBody": map[string]any{
				"required": false,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": schemaRef("ConfigCreateRequest"),
					},
				},
			},
			"responses": map[string]any{
				"200": jsonResponse("Config global preview.", "ParsedConfigResponse"),
			},
		},
	}
}

func resourceItemPath(singularName string, resourceSchemaName string, updateSchemaName string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "Get " + singularName,
			"description": "Returns one stored " + singularName + " by id.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses": map[string]any{
				"200": jsonResponse("Resource found.", resourceSchemaName),
			},
		},
		"put": map[string]any{
			"summary":     "Update " + singularName,
			"description": "Updates name and/or raw section content. `version` only increases when the raw section changes.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": false,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": schemaRef(updateSchemaName),
					},
				},
			},
			"responses": map[string]any{
				"200": jsonResponse("Resource updated.", resourceSchemaName),
			},
		},
		"delete": map[string]any{
			"summary":     "Delete " + singularName,
			"description": "Deletes the stored " + singularName + ".",
			"parameters":  []map[string]any{idPathParameter()},
			"responses": map[string]any{
				"204": map[string]any{"description": "Resource deleted."},
			},
		},
	}
}

func resourceSelectPath(singularName string) map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Select " + singularName,
			"description": "Marks the " + singularName + " as selected.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses": map[string]any{
				"200": jsonResponse("Selection applied.", "SelectionResponse"),
			},
		},
	}
}

func resourceSchema(valueField string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "integer"},
			"name":     map[string]any{"type": "string"},
			valueField: map[string]any{"type": "string"},
			"selected": map[string]any{"type": "boolean"},
			"version":  map[string]any{"type": "integer"},
		},
	}
}

func configResourceSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":           map[string]any{"type": "integer"},
			"name":         map[string]any{"type": "string"},
			"global":       map[string]any{"type": "string"},
			"parsedGlobal": parsedGlobalSchema(),
			"parseError":   map[string]any{"type": "string"},
			"selected":     map[string]any{"type": "boolean"},
			"version":      map[string]any{"type": "integer"},
		},
	}
}

func configMutationSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":         map[string]any{"type": "string"},
			"global":       map[string]any{"type": "string"},
			"parsedGlobal": parsedGlobalSchema(),
		},
	}
}

func parsedGlobalSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           globalInputSchemaProperties(),
	}
}

func globalInputSchemaProperties() map[string]any {
	typ := reflect.TypeOf(daeConfig.Global{})
	properties := make(map[string]any, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		mapping := field.Tag.Get("mapstructure")
		if mapping == "" {
			continue
		}
		properties[snakeToLowerCamel(mapping)] = globalInputFieldSchema(field.Type)
	}
	return properties
}

func globalInputFieldSchema(typ reflect.Type) map[string]any {
	if typ == reflect.TypeOf(time.Duration(0)) {
		return map[string]any{"type": "string"}
	}
	switch typ.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.String {
			return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		return map[string]any{"type": "integer", "minimum": 0}
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		return map[string]any{"type": "integer"}
	}
	return map[string]any{"type": "string"}
}

func dnsResourceSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":        map[string]any{"type": "integer"},
			"name":      map[string]any{"type": "string"},
			"dns":       map[string]any{"type": "string"},
			"parsedDns": schemaRef("ParsedDNSResponse"),
			"selected":  map[string]any{"type": "boolean"},
			"version":   map[string]any{"type": "integer"},
		},
	}
}

func routingResourceSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":              map[string]any{"type": "integer"},
			"name":            map[string]any{"type": "string"},
			"routing":         map[string]any{"type": "string"},
			"parsedRouting":   schemaRef("ParsedRoutingResponse"),
			"referenceGroups": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"selected":        map[string]any{"type": "boolean"},
			"version":         map[string]any{"type": "integer"},
		},
	}
}

func listSchema(resourceSchemaName string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":  "array",
				"items": schemaRef(resourceSchemaName),
			},
		},
	}
}

func resourceMutationSchema(valueField string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":     map[string]any{"type": "string"},
			valueField: map[string]any{"type": "string"},
		},
	}
}

func parsedSectionPath(previewName string, responseSchemaName string) map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Parse " + previewName,
			"description": "Parses a raw editor section and returns a structured preview.",
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": schemaRef("ParsedSectionRequest"),
					},
				},
			},
			"responses": map[string]any{
				"200": jsonResponse("Parsed preview.", responseSchemaName),
			},
		},
	}
}

func jsonResponse(description string, schemaName string) map[string]any {
	return map[string]any{
		"description": description,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": schemaRef(schemaName),
			},
		},
	}
}

func schemaRef(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func idPathParameter() map[string]any {
	return map[string]any{
		"name":     "id",
		"in":       "path",
		"required": true,
		"schema":   map[string]any{"type": "integer"},
	}
}

func nodeCollectionPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List nodes",
			"description": "Returns nodes with optional filters for id, subscription, independent state, and simple pagination.",
			"parameters": []map[string]any{
				{"name": "id", "in": "query", "schema": map[string]any{"type": "integer"}},
				{"name": "subscriptionId", "in": "query", "schema": map[string]any{"type": "integer"}},
				{"name": "independent", "in": "query", "schema": map[string]any{"type": "boolean"}},
				{"name": "afterId", "in": "query", "schema": map[string]any{"type": "integer"}},
				{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "maximum": maxListLimit}},
			},
			"responses": map[string]any{
				"200": jsonResponse("Node list.", "NodeList"),
			},
		},
		"post": map[string]any{
			"summary":     "Import independent nodes",
			"description": "Imports one or more nodes that do not belong to a subscription.",
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaRef("NodeImportRequest")},
				},
			},
			"responses": map[string]any{
				"200": jsonResponse("Node import results.", "NodeImportResultList"),
			},
		},
		"delete": map[string]any{
			"summary":     "Delete nodes",
			"description": "Deletes one or more nodes by id.",
			"requestBody": map[string]any{
				"required": false,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaRef("BatchIDsRequest")},
				},
			},
			"parameters": []map[string]any{
				{"name": "ids", "in": "query", "schema": map[string]any{"type": "string"}},
			},
			"responses": map[string]any{
				"200": jsonResponse("Nodes deleted.", "CountResponse"),
			},
		},
	}
}

func nodeItemPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "Get node",
			"description": "Returns one node by id.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses":   map[string]any{"200": jsonResponse("Node found.", "NodeResource")},
		},
		"put": map[string]any{
			"summary":     "Update node",
			"description": "Updates a node link and/or tag.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaRef("NodeUpdateRequest")},
				},
			},
			"responses": map[string]any{"200": jsonResponse("Node updated.", "NodeResource")},
		},
		"delete": map[string]any{
			"summary":     "Delete node",
			"description": "Deletes a node by id.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses":   map[string]any{"204": map[string]any{"description": "Node deleted."}},
		},
	}
}

func nodeLatenciesPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "Query node latencies",
			"description": "Returns cached/runtime node latency results. Optional `ids` accepts a comma-separated id list.",
			"parameters": []map[string]any{
				{"name": "ids", "in": "query", "schema": map[string]any{"type": "string"}},
			},
			"responses": map[string]any{"200": jsonResponse("Node latencies.", "NodeLatencyList")},
		},
		"post": map[string]any{
			"summary":     "Test node latencies",
			"description": "Triggers latency probes for all or selected node ids.",
			"requestBody": map[string]any{
				"required": false,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
							},
						},
					},
				},
			},
			"responses": map[string]any{"200": jsonResponse("Node latency probes finished.", "NodeLatencyList")},
		},
	}
}

func subscriptionCollectionPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List subscriptions",
			"description": "Returns subscriptions with node counts. Optional `id` query filters to one row. Optional `expand=nodes` embeds full node lists.",
			"parameters": []map[string]any{
				{"name": "id", "in": "query", "schema": map[string]any{"type": "integer"}},
				{"name": "expand", "in": "query", "schema": map[string]any{"type": "string"}},
			},
			"responses": map[string]any{"200": jsonResponse("Subscription list.", "SubscriptionList")},
		},
		"post": map[string]any{
			"summary":     "Import subscription",
			"description": "Fetches a subscription, resolves node links, stores the subscription, and imports nodes.",
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaRef("SubscriptionCreateRequest")},
				},
			},
			"responses": map[string]any{"201": jsonResponse("Subscription imported.", "SubscriptionImportResult")},
		},
		"delete": map[string]any{
			"summary":     "Delete subscriptions",
			"description": "Deletes one or more subscriptions by id.",
			"requestBody": map[string]any{
				"required": false,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaRef("BatchIDsRequest")},
				},
			},
			"parameters": []map[string]any{
				{"name": "ids", "in": "query", "schema": map[string]any{"type": "string"}},
			},
			"responses": map[string]any{"200": jsonResponse("Subscriptions deleted.", "CountResponse")},
		},
	}
}

func subscriptionItemPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "Get subscription",
			"description": "Returns one subscription by id.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses":   map[string]any{"200": jsonResponse("Subscription found.", "SubscriptionResource")},
		},
		"put": map[string]any{
			"summary":     "Update subscription",
			"description": "Updates subscription link, tag, and/or cron settings.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaRef("SubscriptionUpdateRequest")},
				},
			},
			"responses": map[string]any{"200": jsonResponse("Subscription updated.", "SubscriptionResource")},
		},
		"delete": map[string]any{
			"summary":     "Delete subscription",
			"description": "Deletes a subscription and its imported nodes.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses":   map[string]any{"204": map[string]any{"description": "Subscription deleted."}},
		},
	}
}

func subscriptionNodesPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List subscription nodes",
			"description": "Returns nodes that belong to one subscription with optional simple cursor pagination.",
			"parameters": []map[string]any{
				idPathParameter(),
				{"name": "afterId", "in": "query", "schema": map[string]any{"type": "integer"}},
				{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "maximum": maxListLimit}},
			},
			"responses": map[string]any{
				"200": jsonResponse("Subscription nodes.", "NodeList"),
			},
		},
	}
}

func nodeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":             map[string]any{"type": "integer"},
			"link":           map[string]any{"type": "string"},
			"name":           map[string]any{"type": "string"},
			"address":        map[string]any{"type": "string"},
			"protocol":       map[string]any{"type": "string"},
			"transport":      map[string]any{"type": "string"},
			"tag":            map[string]any{"type": "string"},
			"subscriptionId": map[string]any{"type": "integer"},
		},
	}
}

func nodeListSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items":       map[string]any{"type": "array", "items": schemaRef("NodeResource")},
			"totalCount":  map[string]any{"type": "integer"},
			"nextAfterId": map[string]any{"type": "integer"},
		},
	}
}

func nodeImportRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rollbackError": map[string]any{"type": "boolean"},
			"args": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"link": map[string]any{"type": "string"},
						"tag":  map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

func nodeImportResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"link":  map[string]any{"type": "string"},
			"error": map[string]any{"type": "string"},
			"node":  schemaRef("NodeResource"),
		},
	}
}

func nodeUpdateRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"link": map[string]any{"type": "string"},
			"tag":  map[string]any{"type": "string"},
		},
	}
}

func nodeLatencySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":        map[string]any{"type": "integer"},
			"latencyMs": map[string]any{"type": "integer"},
			"alive":     map[string]any{"type": "boolean"},
			"testedAt":  map[string]any{"type": "string", "format": "date-time"},
			"message":   map[string]any{"type": "string"},
		},
	}
}

func subscriptionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":         map[string]any{"type": "integer"},
			"updatedAt":  map[string]any{"type": "string", "format": "date-time"},
			"link":       map[string]any{"type": "string"},
			"cronExp":    map[string]any{"type": "string"},
			"cronEnable": map[string]any{"type": "boolean"},
			"status":     map[string]any{"type": "string"},
			"info":       map[string]any{"type": "string"},
			"tag":        map[string]any{"type": "string"},
			"nodeCount":  map[string]any{"type": "integer"},
			"nodes":      schemaRef("NodeList"),
		},
	}
}

func subscriptionCreateRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"link":          map[string]any{"type": "string"},
			"tag":           map[string]any{"type": "string"},
			"rollbackError": map[string]any{"type": "boolean"},
		},
	}
}

func subscriptionUpdateRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"link":       map[string]any{"type": "string"},
			"tag":        map[string]any{"type": "string"},
			"cronExp":    map[string]any{"type": "string"},
			"cronEnable": map[string]any{"type": "boolean"},
		},
	}
}

func subscriptionImportResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"link":             map[string]any{"type": "string"},
			"nodeImportResult": map[string]any{"type": "array", "items": schemaRef("NodeImportResult")},
			"subscription":     schemaRef("SubscriptionResource"),
		},
	}
}

func generalStatePath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "Read runtime state",
			"description": "Returns running/modified/version state for the current dae runtime.",
			"responses":   map[string]any{"200": jsonResponse("Runtime state.", "RuntimeStateResource")},
		},
	}
}

func generalInterfacesPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List interfaces",
			"description": "Returns system network interfaces with addresses and default routes.",
			"parameters": []map[string]any{
				{"name": "up", "in": "query", "schema": map[string]any{"type": "boolean"}},
				{"name": "onlyGlobalScope", "in": "query", "schema": map[string]any{"type": "boolean"}},
			},
			"responses": map[string]any{"200": jsonResponse("Interface list.", "InterfaceList")},
		},
	}
}

func generalCacheStatsPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "Read cache stats",
			"description": "Returns lightweight cache and queue counts for the active control plane.",
			"responses":   map[string]any{"200": jsonResponse("Cache stats.", "CacheStatsResource")},
		},
	}
}

func groupCollectionPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List groups",
			"description": "Returns groups with node and subscription bindings. Optional filters: `id`, `name`.",
			"parameters": []map[string]any{
				{"name": "id", "in": "query", "schema": map[string]any{"type": "integer"}},
				{"name": "name", "in": "query", "schema": map[string]any{"type": "string"}},
			},
			"responses": map[string]any{"200": jsonResponse("Group list.", "GroupList")},
		},
		"post": map[string]any{
			"summary":     "Create group",
			"description": "Creates a group with policy and policy params.",
			"requestBody": map[string]any{
				"required": true,
				"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("GroupCreateRequest")}},
			},
			"responses": map[string]any{"201": jsonResponse("Group created.", "GroupResource")},
		},
	}
}

func groupItemPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "Get group",
			"description": "Returns one group by id.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses":   map[string]any{"200": jsonResponse("Group found.", "GroupResource")},
		},
		"put": map[string]any{
			"summary":     "Update group",
			"description": "Renames a group and/or updates its policy and policy params.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": true,
				"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("GroupUpdateRequest")}},
			},
			"responses": map[string]any{"200": jsonResponse("Group updated.", "GroupResource")},
		},
		"delete": map[string]any{
			"summary":     "Delete group",
			"description": "Deletes a group and its bindings.",
			"parameters":  []map[string]any{idPathParameter()},
			"responses":   map[string]any{"204": map[string]any{"description": "Group deleted."}},
		},
	}
}

func groupNodesPath() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Add nodes to group",
			"description": "Adds node bindings to a group.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": true,
				"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("GroupNodesRequest")}},
			},
			"responses": map[string]any{"200": jsonResponse("Nodes added.", "CountResponse")},
		},
		"delete": map[string]any{
			"summary":     "Remove nodes from group",
			"description": "Removes node bindings from a group.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": true,
				"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("GroupNodesRequest")}},
			},
			"responses": map[string]any{"200": jsonResponse("Nodes removed.", "CountResponse")},
		},
	}
}

func groupSubscriptionsPath() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Add subscriptions to group",
			"description": "Adds subscription bindings to a group with an optional shared name filter regex.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": true,
				"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("GroupSubscriptionsRequest")}},
			},
			"responses": map[string]any{"200": jsonResponse("Subscriptions added.", "CountResponse")},
		},
		"delete": map[string]any{
			"summary":     "Remove subscriptions from group",
			"description": "Removes subscription bindings from a group.",
			"parameters":  []map[string]any{idPathParameter()},
			"requestBody": map[string]any{
				"required": true,
				"content":  map[string]any{"application/json": map[string]any{"schema": schemaRef("GroupSubscriptionsRequest")}},
			},
			"responses": map[string]any{"200": jsonResponse("Subscriptions removed.", "CountResponse")},
		},
	}
}

func groupSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":            map[string]any{"type": "integer"},
			"name":          map[string]any{"type": "string"},
			"policy":        map[string]any{"type": "string"},
			"policyParams":  map[string]any{"type": "array", "items": schemaRef("ParamResource")},
			"nodes":         map[string]any{"type": "array", "items": schemaRef("NodeResource")},
			"subscriptions": map[string]any{"type": "array", "items": schemaRef("GroupSubscriptionResource")},
			"version":       map[string]any{"type": "integer"},
		},
	}
}

func groupCreateRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":         map[string]any{"type": "string"},
			"policy":       map[string]any{"type": "string"},
			"policyParams": map[string]any{"type": "array", "items": schemaRef("ParamResource")},
		},
	}
}

func groupUpdateRequestSchema() map[string]any {
	return groupCreateRequestSchema()
}

func groupNodesRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"nodeIds": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		},
	}
}

func groupSubscriptionsRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subscriptionIds": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			"nameFilterRegex": map[string]any{"type": "string"},
		},
	}
}
