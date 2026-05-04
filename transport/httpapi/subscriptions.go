/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/orchestrator"
)

type subscriptionResource struct {
	ID         uint              `json:"id"`
	UpdatedAt  string            `json:"updatedAt"`
	Link       string            `json:"link"`
	CronExp    string            `json:"cronExp"`
	CronEnable bool              `json:"cronEnable"`
	Status     string            `json:"status"`
	Info       string            `json:"info"`
	Tag        *string           `json:"tag,omitempty"`
	NodeCount  int64             `json:"nodeCount"`
	Nodes      *nodeListResponse `json:"nodes,omitempty"`
}

type subscriptionCreateRequest struct {
	Link          string  `json:"link"`
	Tag           *string `json:"tag,omitempty"`
	RollbackError bool    `json:"rollbackError"`
}

type subscriptionUpdateRequest struct {
	Link       *string `json:"link"`
	Tag        *string `json:"tag"`
	CronExp    *string `json:"cronExp"`
	CronEnable *bool   `json:"cronEnable"`
}

type subscriptionImportResultResponse struct {
	Link             string                     `json:"link"`
	NodeImportResult []nodeImportResultResponse `json:"nodeImportResult"`
	Subscription     subscriptionResource       `json:"subscription"`
}

func handleSubscriptions(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id, hasID := parseOptionalUint(r.URL.Query().Get("id"))
		expandNodes := hasExpandValue(r.URL.Query().Get("expand"), "nodes")
		var idPtr *uint
		if hasID {
			idPtr = &id
		}
		models, err := orchestrator.ListSubscriptions(r.Context(), idPtr)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		counts, err := subscriptionNodeCounts(r.Context(), subscriptionIDs(models))
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		nodesBySubscriptionID := map[uint]nodeListResponse{}
		if expandNodes {
			nodesBySubscriptionID, err = subscriptionNodesByIDs(r.Context(), subscriptionIDs(models))
			if err != nil {
				writeError(rw, http.StatusInternalServerError, err.Error())
				return
			}
		}
		items := make([]subscriptionResource, 0, len(models))
		for _, model := range models {
			resource := toSubscriptionResource(&model, counts[model.ID])
			if expandNodes {
				nodes := nodesBySubscriptionID[model.ID]
				resource.Nodes = &nodes
			}
			items = append(items, resource)
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var req subscriptionCreateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		result, err := orchestrator.ImportSubscription(r.Context(), req.RollbackError, orchestrator.ImportArgument{
			Link: req.Link,
			Tag:  req.Tag,
		})
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		counts, err := subscriptionNodeCounts(r.Context(), []uint{result.Subscription.ID})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, subscriptionImportResultResponse{
			Link:             result.Link,
			NodeImportResult: toNodeImportResultResponses(result.NodeImportResult),
			Subscription:     toSubscriptionResource(result.Subscription, counts[result.Subscription.ID]),
		})
	case http.MethodDelete:
		ids, err := decodeBatchIDsRequest(r)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		removed, err := orchestrator.DeleteSubscriptions(r.Context(), ids)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"removed": removed})
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost+", "+http.MethodDelete)
	}
}

func handleSubscriptionResource(rw http.ResponseWriter, r *http.Request) {
	if id, ok := parseResourceActionPath(r.URL.Path, "subscriptions", "nodes"); ok {
		handleSubscriptionNodes(rw, r, id)
		return
	}
	if strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/") == "subscriptions/refresh" {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}
	if _, ok := parseResourceActionPath(r.URL.Path, "subscriptions", "refresh"); ok {
		handleSubscriptionRefresh(rw, r)
		return
	}
	id, ok := parseResourcePath(r.URL.Path, "subscriptions")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		model, err := orchestrator.GetSubscription(r.Context(), id)
		if err != nil {
			writeModelError(rw, err)
			return
		}
		counts, err := subscriptionNodeCounts(r.Context(), []uint{id})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, toSubscriptionResource(model, counts[id]))
	case http.MethodPut:
		var req subscriptionUpdateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if req.Link == nil && req.Tag == nil && req.CronExp == nil && req.CronEnable == nil {
			writeError(rw, http.StatusBadRequest, "at least one field must be provided")
			return
		}
		model, err := orchestrator.UpdateSubscription(r.Context(), id, orchestrator.SubscriptionUpdateInput{
			Link:       req.Link,
			Tag:        req.Tag,
			CronExp:    req.CronExp,
			CronEnable: req.CronEnable,
		})
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		counts, err := subscriptionNodeCounts(r.Context(), []uint{id})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, toSubscriptionResource(model, counts[id]))
	case http.MethodDelete:
		deleted, err := orchestrator.DeleteSubscription(r.Context(), id)
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

func handleSubscriptionRefresh(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	id, ok := parseResourceActionPath(r.URL.Path, "subscriptions", "refresh")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}
	model, err := orchestrator.RefreshSubscription(r.Context(), id)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	counts, err := subscriptionNodeCounts(r.Context(), []uint{id})
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, toSubscriptionResource(model, counts[id]))
}

func handleSubscriptionNodes(rw http.ResponseWriter, r *http.Request, id uint) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	afterID, hasAfterID := parseOptionalUint(r.URL.Query().Get("afterId"))
	limitValue := parseListLimit(r.URL.Query().Get("limit"))

	var afterIDPtr *uint
	if hasAfterID {
		afterIDPtr = &afterID
	}
	var limitPtr *int
	if limitValue > 0 {
		limitPtr = &limitValue
	}

	models, totalCount, err := orchestrator.ListNodes(r.Context(), nil, &id, nil, afterIDPtr, limitPtr)
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
}

func toSubscriptionResource(model *db.Subscription, nodeCount int64) subscriptionResource {
	return subscriptionResource{
		ID:         model.ID,
		UpdatedAt:  model.UpdatedAt.Format(time.RFC3339Nano),
		Link:       model.Link,
		CronExp:    model.CronExp,
		CronEnable: model.CronEnable,
		Status:     model.Status,
		Info:       model.Info,
		Tag:        model.Tag,
		NodeCount:  nodeCount,
	}
}

func subscriptionIDs(models []db.Subscription) []uint {
	ids := make([]uint, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func subscriptionNodeCounts(ctx context.Context, ids []uint) (map[uint]int64, error) {
	counts := make(map[uint]int64, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}

	type row struct {
		SubscriptionID uint
		Count          int64
	}
	var rows []row
	if err := db.DB(ctx).
		Model(&db.Node{}).
		Select("subscription_id, count(*) as count").
		Where("subscription_id in ?", ids).
		Group("subscription_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.SubscriptionID] = row.Count
	}
	return counts, nil
}

func subscriptionNodesByIDs(ctx context.Context, ids []uint) (map[uint]nodeListResponse, error) {
	results := make(map[uint]nodeListResponse, len(ids))
	if len(ids) == 0 {
		return results, nil
	}

	for _, id := range ids {
		results[id] = nodeListResponse{
			Items:      []nodeResource{},
			TotalCount: 0,
		}
	}

	var models []db.Node
	if err := db.DB(ctx).
		Where("subscription_id in ?", ids).
		Order("id asc").
		Find(&models).Error; err != nil {
		return nil, err
	}

	for _, model := range models {
		if model.SubscriptionID == nil {
			continue
		}
		response := results[*model.SubscriptionID]
		response.Items = append(response.Items, toNodeResource(&model))
		response.TotalCount++
		results[*model.SubscriptionID] = response
	}
	return results, nil
}

func hasExpandValue(raw string, target string) bool {
	for _, part := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(part), target) {
			return true
		}
	}
	return false
}

func parseResourceActionPath(path string, resource string, action string) (uint, bool) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 || parts[0] != resource || parts[2] != action {
		return 0, false
	}
	id, ok := parseOptionalUint(parts[1])
	if !ok {
		return 0, false
	}
	return id, true
}
