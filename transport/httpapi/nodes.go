/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/orchestrator"
	outboundhttp "github.com/daeuniverse/outbound/dialer/http"
	"github.com/daeuniverse/outbound/dialer/shadowsocks"
	"github.com/daeuniverse/outbound/dialer/socks"
	"github.com/daeuniverse/outbound/dialer/trojan"
	"github.com/daeuniverse/outbound/dialer/v2ray"
)

type nodeResource struct {
	ID             uint    `json:"id"`
	Link           string  `json:"link"`
	Name           string  `json:"name"`
	Address        string  `json:"address"`
	Protocol       string  `json:"protocol"`
	Transport      *string `json:"transport,omitempty"`
	Tag            *string `json:"tag,omitempty"`
	SubscriptionID *uint   `json:"subscriptionId,omitempty"`
}

type nodeListResponse struct {
	Items       []nodeResource `json:"items"`
	TotalCount  int64          `json:"totalCount"`
	NextAfterID *uint          `json:"nextAfterId,omitempty"`
}

type nodeImportArgumentRequest struct {
	Link string  `json:"link"`
	Tag  *string `json:"tag,omitempty"`
}

type nodeImportRequest struct {
	RollbackError bool                        `json:"rollbackError"`
	Args          []nodeImportArgumentRequest `json:"args"`
}

type nodeImportResultResponse struct {
	Link  string        `json:"link"`
	Error *string       `json:"error,omitempty"`
	Node  *nodeResource `json:"node,omitempty"`
}

type nodeUpdateRequest struct {
	Link *string `json:"link"`
	Tag  *string `json:"tag"`
}

type nodeLatenciesRequest struct {
	IDs []uint `json:"ids"`
}

type batchIDsRequest struct {
	IDs []uint `json:"ids"`
}

type nodeLatencyResource struct {
	ID        uint    `json:"id"`
	LatencyMs *int32  `json:"latencyMs,omitempty"`
	Alive     bool    `json:"alive"`
	TestedAt  string  `json:"testedAt"`
	Message   *string `json:"message,omitempty"`
}

func handleNodes(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id, _ := parseOptionalUint(r.URL.Query().Get("id"))
		subscriptionID, hasSubscriptionID := parseOptionalUint(r.URL.Query().Get("subscriptionId"))
		independentValue, hasIndependent := parseOptionalBool(r.URL.Query().Get("independent"))
		afterID, hasAfterID := parseOptionalUint(r.URL.Query().Get("afterId"))
		limitValue := parseListLimit(r.URL.Query().Get("limit"))

		var idPtr *uint
		if id != 0 {
			idPtr = &id
		}
		var subscriptionIDPtr *uint
		if hasSubscriptionID {
			subscriptionIDPtr = &subscriptionID
		}
		var independentPtr *bool
		if hasIndependent {
			independentPtr = &independentValue
		} else if subscriptionIDPtr == nil {
			defaultIndependent := true
			independentPtr = &defaultIndependent
		}
		var afterIDPtr *uint
		if hasAfterID {
			afterIDPtr = &afterID
		}
		var limitPtr *int
		if limitValue > 0 {
			limitPtr = &limitValue
		}

		models, totalCount, err := orchestrator.ListNodes(r.Context(), idPtr, subscriptionIDPtr, independentPtr, afterIDPtr, limitPtr)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		items := make([]nodeResource, 0, len(models))
		for _, model := range models {
			items = append(items, toNodeResource(&model))
		}
		response := nodeListResponse{
			Items:      items,
			TotalCount: totalCount,
		}
		if limitPtr != nil && len(models) == *limitPtr {
			nextAfterID := models[len(models)-1].ID
			response.NextAfterID = &nextAfterID
		}
		writeJSON(rw, http.StatusOK, response)
	case http.MethodPost:
		var req nodeImportRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if len(req.Args) == 0 {
			writeError(rw, http.StatusBadRequest, "at least one node import argument must be provided")
			return
		}

		tx := db.BeginTx(r.Context())
		args := make([]orchestrator.ImportArgument, 0, len(req.Args))
		for _, arg := range req.Args {
			args = append(args, orchestrator.ImportArgument{
				Link: arg.Link,
				Tag:  arg.Tag,
			})
		}
		results, err := orchestrator.ImportNodes(tx, req.RollbackError, nil, args)
		if err != nil {
			tx.Rollback()
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if err := tx.Commit().Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": toNodeImportResultResponses(results)})
	case http.MethodDelete:
		ids, err := decodeBatchIDsRequest(r)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		removed, err := orchestrator.DeleteNodes(r.Context(), ids)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"removed": removed})
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost+", "+http.MethodDelete)
	}
}

func handleNodeResource(rw http.ResponseWriter, r *http.Request) {
	if strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/") == "nodes/latencies" {
		handleNodeLatencies(rw, r)
		return
	}
	id, ok := parseResourcePath(r.URL.Path, "nodes")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		model, err := orchestrator.GetNode(r.Context(), id)
		if err != nil {
			writeModelError(rw, err)
			return
		}
		writeJSON(rw, http.StatusOK, toNodeResource(model))
	case http.MethodPut:
		var req nodeUpdateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if req.Link == nil && req.Tag == nil {
			writeError(rw, http.StatusBadRequest, "at least one field must be provided")
			return
		}
		model, err := orchestrator.UpdateNode(r.Context(), id, req.Link, req.Tag)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, toNodeResource(model))
	case http.MethodDelete:
		deleted, err := orchestrator.DeleteNode(r.Context(), id)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if !deleted {
			writeError(rw, http.StatusNotFound, "not found")
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
	}
}

func handleNodeLatencies(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ids, err := parseCSVUintList(r.URL.Query().Get("ids"))
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		results, err := orchestrator.QueryNodeLatencies(r.Context(), ids)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": toNodeLatencyResources(results)})
	case http.MethodPost:
		var req nodeLatenciesRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		results, err := orchestrator.TestNodeLatencies(r.Context(), req.IDs)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": toNodeLatencyResources(results)})
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost)
	}
}

func toNodeResource(model *db.Node) nodeResource {
	return nodeResource{
		ID:             model.ID,
		Link:           model.Link,
		Name:           model.Name,
		Address:        model.Address,
		Protocol:       model.Protocol,
		Transport:      deriveNodeTransport(model.Link, model.Protocol),
		Tag:            model.Tag,
		SubscriptionID: model.SubscriptionID,
	}
}

func deriveNodeTransport(link string, protocol string) *string {
	switch protocol {
	case "http", "https", "socks5":
		return &protocol
	}

	switch {
	case strings.HasPrefix(link, "vmess://"), strings.HasPrefix(link, "vless://"):
		parsed, err := parseV2RayTransport(link)
		if err == nil && parsed != "" {
			return &parsed
		}
	case strings.HasPrefix(link, "trojan://"), strings.HasPrefix(link, "trojan-go://"):
		parsed, err := trojan.ParseTrojanURL(link)
		if err == nil && parsed.Type == "ws" {
			transport := "ws"
			return &transport
		}
	case strings.HasPrefix(link, "ss://"), strings.HasPrefix(link, "shadowsocks://"):
		parsed, err := shadowsocks.ParseSSURL(link)
		if err == nil {
			if parsed.Plugin.Name == "v2ray-plugin" && parsed.Plugin.Opts.Obfs != "" {
				transport := parsed.Plugin.Opts.Obfs
				return &transport
			}
			if parsed.Plugin.Name != "" {
				transport := parsed.Plugin.Name
				return &transport
			}
		}
	case strings.HasPrefix(link, "http://"), strings.HasPrefix(link, "https://"):
		parsed, err := outboundhttp.ParseHTTPURL(link)
		if err == nil && parsed.Protocol != "" {
			transport := parsed.Protocol
			return &transport
		}
	case strings.HasPrefix(link, "socks://"), strings.HasPrefix(link, "socks5://"):
		parsed, err := socks.ParseSocksURL(link)
		if err == nil && parsed.Protocol != "" {
			transport := parsed.Protocol
			return &transport
		}
	}

	return nil
}

func parseV2RayTransport(link string) (string, error) {
	var (
		parsed *v2ray.V2Ray
		err    error
	)
	switch {
	case strings.HasPrefix(link, "vmess://"):
		parsed, err = v2ray.ParseVmessURL(link)
	case strings.HasPrefix(link, "vless://"):
		parsed, err = v2ray.ParseVlessURL(link)
	default:
		return "", nil
	}
	if err != nil || parsed == nil {
		return "", err
	}
	switch parsed.Net {
	case "http", "http2":
		return "h2", nil
	case "websocket":
		return "ws", nil
	default:
		return parsed.Net, nil
	}
}

func toNodeImportResultResponses(results []*orchestrator.NodeImportResult) []nodeImportResultResponse {
	items := make([]nodeImportResultResponse, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		var node *nodeResource
		if result.Node != nil {
			resource := toNodeResource(result.Node)
			node = &resource
		}
		items = append(items, nodeImportResultResponse{
			Link:  result.Link,
			Error: result.Error,
			Node:  node,
		})
	}
	return items
}

func toNodeLatencyResources(results []*orchestrator.NodeLatencyResult) []nodeLatencyResource {
	items := make([]nodeLatencyResource, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		items = append(items, nodeLatencyResource{
			ID:        result.NodeID,
			LatencyMs: result.LatencyMs,
			Alive:     result.Alive,
			TestedAt:  result.TestedAt.Format(time.RFC3339Nano),
			Message:   result.Message,
		})
	}
	return items
}

func parseOptionalUint(raw string) (uint, bool) {
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || parsed == 0 {
		return 0, false
	}
	return uint(parsed), true
}

func parseCSVUintList(raw string) ([]uint, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	items := make([]uint, 0, len(parts))
	for _, part := range parts {
		value, ok := parseOptionalUint(strings.TrimSpace(part))
		if !ok {
			return nil, strconv.ErrSyntax
		}
		items = append(items, value)
	}
	return items, nil
}

func decodeBatchIDsRequest(r *http.Request) ([]uint, error) {
	if ids, err := parseCSVUintList(r.URL.Query().Get("ids")); err != nil {
		return nil, err
	} else if len(ids) > 0 {
		return ids, nil
	}

	var req batchIDsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		return nil, err
	}
	if len(req.IDs) == 0 {
		return nil, strconv.ErrSyntax
	}
	return req.IDs, nil
}
