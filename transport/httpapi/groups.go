/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"net/http"
	"strings"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/orchestrator"
	"github.com/daeuniverse/dae/pkg/config_parser"
)

type paramResource struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

type groupSubscriptionResource struct {
	SubscriptionID  uint           `json:"subscriptionId"`
	NameFilterRegex *string        `json:"nameFilterRegex,omitempty"`
	MatchedCount    int            `json:"matchedCount"`
	MatchedNodes    []nodeResource `json:"matchedNodes"`
	Link            string         `json:"link"`
	Tag             *string        `json:"tag,omitempty"`
}

type groupResource struct {
	ID            uint                        `json:"id"`
	Name          string                      `json:"name"`
	Policy        string                      `json:"policy"`
	PolicyParams  []paramResource             `json:"policyParams"`
	Nodes         []nodeResource              `json:"nodes"`
	Subscriptions []groupSubscriptionResource `json:"subscriptions"`
	Version       uint                        `json:"version"`
}

type groupCreateRequest struct {
	Name         string          `json:"name"`
	Policy       string          `json:"policy"`
	PolicyParams []paramResource `json:"policyParams"`
}

type groupUpdateRequest struct {
	Name         *string         `json:"name"`
	Policy       *string         `json:"policy"`
	PolicyParams []paramResource `json:"policyParams"`
}

type groupSubscriptionsRequest struct {
	SubscriptionIDs []uint  `json:"subscriptionIds"`
	NameFilterRegex *string `json:"nameFilterRegex"`
}

type groupNodesRequest struct {
	NodeIDs []uint `json:"nodeIds"`
}

func handleGroups(rw http.ResponseWriter, r *http.Request) {
	if name, ok := parseGroupByNamePath(r.URL.Path); ok {
		handleGroupByName(rw, r, name)
		return
	}
	switch r.Method {
	case http.MethodGet:
		id, hasID := parseOptionalUint(r.URL.Query().Get("id"))
		name := r.URL.Query().Get("name")
		var idPtr *uint
		if hasID {
			idPtr = &id
		}
		var namePtr *string
		if name != "" {
			namePtr = &name
		}
		models, err := orchestrator.ListGroups(r.Context(), idPtr, namePtr)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		items, err := toGroupResources(models)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var req groupCreateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		model, err := orchestrator.CreateGroup(r.Context(), req.Name, req.Policy, toConfigParams(req.PolicyParams))
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		items, err := toGroupResources([]db.Group{*model})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, items[0])
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost)
	}
}

func handleGroupByName(rw http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	models, err := orchestrator.ListGroups(r.Context(), nil, &name)
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	if len(models) == 0 {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}
	items, err := toGroupResources([]db.Group{models[0]})
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, items[0])
}

func handleGroupResource(rw http.ResponseWriter, r *http.Request) {
	if id, ok := parseResourceActionPath(r.URL.Path, "groups", "subscriptions"); ok {
		handleGroupSubscriptions(rw, r, id)
		return
	}
	if id, ok := parseResourceActionPath(r.URL.Path, "groups", "nodes"); ok {
		handleGroupNodes(rw, r, id)
		return
	}

	id, ok := parseResourcePath(r.URL.Path, "groups")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		model, err := orchestrator.GetGroup(r.Context(), id)
		if err != nil {
			writeModelError(rw, err)
			return
		}
		items, err := toGroupResources([]db.Group{*model})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, items[0])
	case http.MethodPut:
		var req groupUpdateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if req.Name == nil && req.Policy == nil {
			writeError(rw, http.StatusBadRequest, "at least one field must be provided")
			return
		}
		if req.Name != nil {
			if _, err := orchestrator.RenameGroup(r.Context(), id, *req.Name); err != nil {
				writeError(rw, http.StatusBadRequest, err.Error())
				return
			}
		}
		if req.Policy != nil {
			if _, err := orchestrator.SetGroupPolicy(r.Context(), id, *req.Policy, toConfigParams(req.PolicyParams)); err != nil {
				writeError(rw, http.StatusBadRequest, err.Error())
				return
			}
		}
		model, err := orchestrator.GetGroup(r.Context(), id)
		if err != nil {
			writeModelError(rw, err)
			return
		}
		items, err := toGroupResources([]db.Group{*model})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, items[0])
	case http.MethodDelete:
		deleted, err := orchestrator.DeleteGroup(r.Context(), id)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if deleted == 0 {
			writeError(rw, http.StatusNotFound, "not found")
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
	}
}

func handleGroupSubscriptions(rw http.ResponseWriter, r *http.Request, id uint) {
	var req groupSubscriptionsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	switch r.Method {
	case http.MethodPost:
		updated, err := orchestrator.AddGroupSubscriptions(r.Context(), id, req.SubscriptionIDs, req.NameFilterRegex)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"updated": updated})
	case http.MethodDelete:
		updated, err := orchestrator.DeleteGroupSubscriptions(r.Context(), id, req.SubscriptionIDs)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"updated": updated})
	default:
		writeMethodNotAllowed(rw, http.MethodPost+", "+http.MethodDelete)
	}
}

func handleGroupNodes(rw http.ResponseWriter, r *http.Request, id uint) {
	var req groupNodesRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	switch r.Method {
	case http.MethodPost:
		updated, err := orchestrator.AddGroupNodes(r.Context(), id, req.NodeIDs)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"updated": updated})
	case http.MethodDelete:
		updated, err := orchestrator.DeleteGroupNodes(r.Context(), id, req.NodeIDs)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"updated": updated})
	default:
		writeMethodNotAllowed(rw, http.MethodPost+", "+http.MethodDelete)
	}
}

func toGroupResources(models []db.Group) ([]groupResource, error) {
	items := make([]groupResource, 0, len(models))
	for _, model := range models {
		nodeResources := make([]nodeResource, 0, len(model.Node))
		for _, node := range model.Node {
			nodeResources = append(nodeResources, toNodeResource(&node))
		}

		bindingResources := make([]groupSubscriptionResource, 0, len(model.SubscriptionBindings))
		for _, binding := range model.SubscriptionBindings {
			matchedNodes, err := orchestrator.MatchedNodesForGroupBinding(&binding)
			if err != nil {
				return nil, err
			}
			matchedResources := make([]nodeResource, 0, len(matchedNodes))
			for _, matchedNode := range matchedNodes {
				matchedResources = append(matchedResources, toNodeResource(&matchedNode))
			}
			bindingResources = append(bindingResources, groupSubscriptionResource{
				SubscriptionID:  binding.SubscriptionID,
				NameFilterRegex: binding.NameFilterRegex,
				MatchedCount:    len(matchedNodes),
				MatchedNodes:    matchedResources,
				Link:            binding.Subscription.Link,
				Tag:             binding.Subscription.Tag,
			})
		}

		paramResources := make([]paramResource, 0, len(model.PolicyParams))
		for _, param := range model.PolicyParams {
			paramResources = append(paramResources, paramResource{
				Key: param.Key,
				Val: param.Value,
			})
		}

		items = append(items, groupResource{
			ID:            model.ID,
			Name:          model.Name,
			Policy:        model.Policy,
			PolicyParams:  paramResources,
			Nodes:         nodeResources,
			Subscriptions: bindingResources,
			Version:       model.Version,
		})
	}
	return items, nil
}

func toConfigParams(params []paramResource) []config_parser.Param {
	items := make([]config_parser.Param, 0, len(params))
	for _, param := range params {
		items = append(items, config_parser.Param{
			Key: param.Key,
			Val: param.Val,
		})
	}
	return items
}

func parseGroupByNamePath(path string) (string, bool) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 || parts[0] != "groups" || parts[1] != "by-name" || strings.TrimSpace(parts[2]) == "" {
		return "", false
	}
	return parts[2], true
}
