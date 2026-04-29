/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/orchestrator"
	"github.com/tidwall/sjson"
	"gorm.io/gorm"
)

type userResource struct {
	Username string  `json:"username"`
	Name     *string `json:"name,omitempty"`
	Avatar   *string `json:"avatar,omitempty"`
}

type userPatchRequest struct {
	Username    *string `json:"username"`
	Name        *string `json:"name"`
	Avatar      *string `json:"avatar"`
	ClearName   bool    `json:"clearName"`
	ClearAvatar bool    `json:"clearAvatar"`
}

type passwordChangeRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type jsonStorageSetRequest struct {
	Paths  []string `json:"paths"`
	Values []string `json:"values"`
}

type jsonStorageRemoveRequest struct {
	Paths []string `json:"paths"`
}

type ensureDefaultResourcesRequest struct {
	ConfigName   string          `json:"configName"`
	Global       map[string]any  `json:"global"`
	DNSName      string          `json:"dnsName"`
	DNS          string          `json:"dns"`
	RoutingName  string          `json:"routingName"`
	Routing      string          `json:"routing"`
	GroupName    string          `json:"groupName"`
	Policy       string          `json:"policy"`
	PolicyParams []paramResource `json:"policyParams"`
	Mode         string          `json:"mode"`
}

type ensureDefaultResourcesResponse struct {
	DefaultConfigID  string `json:"defaultConfigID"`
	DefaultRoutingID string `json:"defaultRoutingID"`
	DefaultDNSID     string `json:"defaultDNSID"`
	DefaultGroupID   string `json:"defaultGroupID"`
	Mode             string `json:"mode"`
}

func handleCurrentUser(rw http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromContext(r)
	if !ok {
		writeError(rw, http.StatusUnauthorized, "authentication required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(rw, http.StatusOK, toUserResource(user))
	case http.MethodPatch:
		var req userPatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		input := orchestrator.UserUpdateInput{
			Username:    req.Username,
			Name:        req.Name,
			ClearName:   req.ClearName,
			Avatar:      req.Avatar,
			ClearAvatar: req.ClearAvatar,
		}
		if _, err := orchestrator.UpdateUser(r.Context(), user, input); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, toUserResource(user))
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPatch)
	}
}

func handleCurrentUserPassword(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	user, ok := currentUserFromContext(r)
	if !ok {
		writeError(rw, http.StatusUnauthorized, "authentication required")
		return
	}
	var req passwordChangeRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	token, err := orchestrator.UpdatePassword(r.Context(), user, orchestrator.PasswordUpdateInput{
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
	}, false)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"token": token})
}

func handleCurrentUserDefaultResources(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	user, ok := currentUserFromContext(r)
	if !ok {
		writeError(rw, http.StatusUnauthorized, "authentication required")
		return
	}

	var req ensureDefaultResourcesRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}

	tx := db.BeginTx(r.Context())
	resp, err := ensureDefaultResources(tx, user, &req)
	if err != nil {
		tx.Rollback()
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	if err := tx.Commit().Error; err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, resp)
}

func handleCurrentUserStorage(rw http.ResponseWriter, r *http.Request) {
	user, ok := currentUserFromContext(r)
	if !ok {
		writeError(rw, http.StatusUnauthorized, "authentication required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		values := orchestrator.QueryJSONStorage(user, r.URL.Query()["path"])
		writeJSON(rw, http.StatusOK, map[string]any{"values": values})
	case http.MethodPut:
		var req jsonStorageSetRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := orchestrator.SetJSONStorage(r.Context(), user, req.Paths, req.Values)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"updated": updated})
	case http.MethodDelete:
		var req jsonStorageRemoveRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		removed, err := orchestrator.RemoveJSONStorage(r.Context(), user, req.Paths)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"removed": removed})
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
	}
}

func toUserResource(user *db.User) userResource {
	return userResource{
		Username: user.Username,
		Name:     user.Name,
		Avatar:   user.Avatar,
	}
}

func ensureDefaultResources(tx *gorm.DB, user *db.User, req *ensureDefaultResourcesRequest) (ensureDefaultResourcesResponse, error) {
	stored := orchestrator.QueryJSONStorage(user, []string{"defaultConfigID", "defaultRoutingID", "defaultDNSID", "defaultGroupID"})
	var storedConfigID, storedRoutingID, storedDNSID, storedGroupID string
	if len(stored) >= 4 {
		storedConfigID, storedRoutingID, storedDNSID, storedGroupID = stored[0], stored[1], stored[2], stored[3]
	}

	configModel, err := ensureConfigResource(tx, storedConfigID, req.ConfigName, req.Global)
	if err != nil {
		return ensureDefaultResourcesResponse{}, err
	}
	dnsModel, err := ensureDNSResource(tx, storedDNSID, req.DNSName, req.DNS)
	if err != nil {
		return ensureDefaultResourcesResponse{}, err
	}
	routingModel, err := ensureRoutingResource(tx, storedRoutingID, req.RoutingName, req.Routing)
	if err != nil {
		return ensureDefaultResourcesResponse{}, err
	}
	groupModel, err := ensureGroupResource(tx, storedGroupID, req.GroupName, req.Policy, req.PolicyParams)
	if err != nil {
		return ensureDefaultResourcesResponse{}, err
	}

	paths := []string{"defaultConfigID", "defaultRoutingID", "defaultDNSID", "defaultGroupID", "mode"}
	values := []string{
		strconv.FormatUint(uint64(configModel.ID), 10),
		strconv.FormatUint(uint64(routingModel.ID), 10),
		strconv.FormatUint(uint64(dnsModel.ID), 10),
		strconv.FormatUint(uint64(groupModel.ID), 10),
		req.Mode,
	}
	if err := setJSONStorageWithTx(tx, user, paths, values); err != nil {
		return ensureDefaultResourcesResponse{}, err
	}

	return ensureDefaultResourcesResponse{
		DefaultConfigID:  values[0],
		DefaultRoutingID: values[1],
		DefaultDNSID:     values[2],
		DefaultGroupID:   values[3],
		Mode:             req.Mode,
	}, nil
}

func ensureConfigResource(tx *gorm.DB, storedID string, name string, global map[string]any) (*db.Config, error) {
	if id, ok := parseStoredUint(storedID); ok {
		if model, err := getConfigModel(tx, id); err == nil {
			return model, nil
		}
	}
	var existing db.Config
	if err := tx.Where("name = ?", name).First(&existing).Error; err == nil {
		return &existing, nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	globalSection, err := buildConfigGlobalSectionForCreate(nil, global)
	if err != nil {
		return nil, err
	}
	model := db.Config{Name: name, Global: globalSection}
	if _, err := engine.Default().ParseConfig(&model.Global, nil, nil); err != nil {
		return nil, err
	}
	if err := tx.Create(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func ensureDNSResource(tx *gorm.DB, storedID string, name string, dns string) (*db.Dns, error) {
	if id, ok := parseStoredUint(storedID); ok {
		if model, err := getDNSModel(tx, id); err == nil {
			return model, nil
		}
	}
	var existing db.Dns
	if err := tx.Where("name = ?", name).First(&existing).Error; err == nil {
		return &existing, nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	model := db.Dns{Name: name, Dns: normalizeEnsureSection("dns", dns, engine.Default().EmptyDnsSection())}
	if _, err := engine.Default().ParseConfig(nil, &model.Dns, nil); err != nil {
		return nil, err
	}
	if err := tx.Create(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func ensureRoutingResource(tx *gorm.DB, storedID string, name string, routing string) (*db.Routing, error) {
	if id, ok := parseStoredUint(storedID); ok {
		if model, err := getRoutingModel(tx, id); err == nil {
			return model, nil
		}
	}
	var existing db.Routing
	if err := tx.Where("name = ?", name).First(&existing).Error; err == nil {
		return &existing, nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	model := db.Routing{Name: name, Routing: normalizeEnsureSection("routing", routing, engine.Default().EmptyRoutingSection())}
	if _, err := engine.Default().ParseConfig(nil, nil, &model.Routing); err != nil {
		return nil, err
	}
	if err := tx.Create(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func ensureGroupResource(tx *gorm.DB, storedID string, name string, policy string, policyParams []paramResource) (*db.Group, error) {
	if id, ok := parseStoredUint(storedID); ok {
		var model db.Group
		if err := tx.First(&model, id).Error; err == nil {
			return &model, nil
		}
	}
	var existing db.Group
	if err := tx.Where("name = ?", name).First(&existing).Error; err == nil {
		return &existing, nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	if err := common.ValidateId(name); err != nil {
		return nil, err
	}
	params := make([]db.GroupPolicyParam, len(policyParams))
	for i := range params {
		params[i] = db.GroupPolicyParam{
			Key:   policyParams[i].Key,
			Value: policyParams[i].Val,
		}
	}
	model := db.Group{
		Name:         name,
		Policy:       policy,
		PolicyParams: params,
	}
	if err := tx.Create(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func parseStoredUint(value string) (uint, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(parsed), true
}

func setJSONStorageWithTx(tx *gorm.DB, user *db.User, paths []string, values []string) error {
	if len(paths) != len(values) {
		return fmt.Errorf("len(paths) != len(values)")
	}
	var err error
	for i := range paths {
		user.JsonStorage, err = sjson.Set(user.JsonStorage, paths[i], values[i])
		if err != nil {
			return err
		}
	}
	return tx.Model(user).Update("json_storage", user.JsonStorage).Error
}

func normalizeEnsureSection(name string, body string, fallback string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return fallback
	}
	if strings.HasPrefix(trimmed, name+" ") || strings.HasPrefix(trimmed, name+"{") {
		return trimmed
	}
	return fmt.Sprintf("%s {\n%s\n}", name, trimmed)
}
