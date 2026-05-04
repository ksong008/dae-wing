/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/orchestrator"
	"gorm.io/gorm"
)

type configResource struct {
	ID           uint           `json:"id"`
	Name         string         `json:"name"`
	Global       string         `json:"global"`
	ParsedGlobal map[string]any `json:"parsedGlobal,omitempty"`
	ParseError   string         `json:"parseError,omitempty"`
	Selected     bool           `json:"selected"`
	Version      uint           `json:"version"`
}

type dnsResource struct {
	ID        uint               `json:"id"`
	Name      string             `json:"name"`
	DNS       string             `json:"dns"`
	ParsedDNS *parsedDNSResponse `json:"parsedDns,omitempty"`
	Selected  bool               `json:"selected"`
	Version   uint               `json:"version"`
}

type routingResource struct {
	ID              uint                   `json:"id"`
	Name            string                 `json:"name"`
	Routing         string                 `json:"routing"`
	ParsedRouting   *parsedRoutingResponse `json:"parsedRouting,omitempty"`
	ReferenceGroups []string               `json:"referenceGroups,omitempty"`
	Selected        bool                   `json:"selected"`
	Version         uint                   `json:"version"`
}

type configMutationRequest struct {
	Name         *string        `json:"name"`
	Global       *string        `json:"global"`
	ParsedGlobal map[string]any `json:"parsedGlobal"`
}

type dnsMutationRequest struct {
	Name *string `json:"name"`
	DNS  *string `json:"dns"`
}

type routingMutationRequest struct {
	Name    *string `json:"name"`
	Routing *string `json:"routing"`
}

func handleConfigs(rw http.ResponseWriter, r *http.Request) {
	if strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/") == "configs/flat-desc" {
		handleConfigFlatDesc(rw, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		q := db.DB(r.Context()).Model(&db.Config{}).Order("id asc")
		if id, ok := parseOptionalUint(r.URL.Query().Get("id")); ok {
			q = q.Where("id = ?", id)
		}
		if selected, ok := parseOptionalBool(r.URL.Query().Get("selected")); ok {
			q = q.Where("selected = ?", selected)
		}
		var models []db.Config
		if err := q.Find(&models).Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		expandParsed := shouldExpandParsed(r)
		items := make([]configResource, 0, len(models))
		for _, model := range models {
			item := buildConfigResourceRaw(&model)
			if expandParsed {
				var err error
				item, err = buildConfigResource(&model)
				if err != nil {
					writeError(rw, http.StatusInternalServerError, err.Error())
					return
				}
			}
			items = append(items, item)
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var req configMutationRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}

		globalSection, err := buildConfigGlobalSectionForCreate(req.Global, req.ParsedGlobal)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		model := db.Config{
			Name:   optionalString(req.Name),
			Global: globalSection,
		}
		if _, err := engine.Default().ParseConfig(&model.Global, nil, nil); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if err := db.DB(r.Context()).Create(&model).Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		item, err := buildConfigResource(&model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, item)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost)
	}
}

func handleConfigFlatDesc(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"items": engine.Default().ExportFlatDesc()})
}

func handleConfigResource(rw http.ResponseWriter, r *http.Request) {
	if _, ok := parseResourceSelectionPath(r.URL.Path, "configs"); ok {
		handleConfigSelection(rw, r)
		return
	}
	id, ok := parseResourcePath(r.URL.Path, "configs")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		model, err := getConfigModel(db.DB(r.Context()), id)
		if err != nil {
			writeModelError(rw, err)
			return
		}
		item, err := buildConfigResource(model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, item)
	case http.MethodPut:
		var req configMutationRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if req.Name == nil && req.Global == nil && req.ParsedGlobal == nil {
			writeError(rw, http.StatusBadRequest, "at least one field must be provided")
			return
		}
		tx := db.BeginTx(r.Context())
		model, err := getConfigModel(tx, id)
		if err != nil {
			tx.Rollback()
			writeModelError(rw, err)
			return
		}
		updates := map[string]any{}
		if req.Name != nil {
			updates["name"] = *req.Name
		}
		if req.Global != nil || req.ParsedGlobal != nil {
			globalSection, err := buildConfigGlobalSectionForUpdate(model.Global, req.Global, req.ParsedGlobal)
			if err != nil {
				tx.Rollback()
				writeError(rw, http.StatusBadRequest, err.Error())
				return
			}
			if _, err := engine.Default().ParseConfig(&globalSection, nil, nil); err != nil {
				tx.Rollback()
				writeError(rw, http.StatusBadRequest, err.Error())
				return
			}
			updates["global"] = globalSection
			updates["version"] = gorm.Expr("version + 1")
		}
		if err := tx.Model(&db.Config{ID: id}).Updates(updates).Error; err != nil {
			tx.Rollback()
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		model, err = getConfigModel(tx, id)
		if err != nil {
			tx.Rollback()
			writeModelError(rw, err)
			return
		}
		if err := tx.Commit().Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		item, err := buildConfigResource(model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, item)
	case http.MethodDelete:
		q := db.DB(r.Context()).Delete(&db.Config{}, id)
		if q.Error != nil {
			writeError(rw, http.StatusInternalServerError, q.Error.Error())
			return
		}
		if q.RowsAffected == 0 {
			writeError(rw, http.StatusNotFound, "no such config")
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
	}
}

func handleConfigSelection(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	id, ok := parseResourceSelectionPath(r.URL.Path, "configs")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}
	applied, err := orchestrator.SelectConfig(r.Context(), id)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"applied": applied, "selectedId": id})
}

func handleDNSResources(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := db.DB(r.Context()).Model(&db.Dns{}).Order("id asc")
		if id, ok := parseOptionalUint(r.URL.Query().Get("id")); ok {
			q = q.Where("id = ?", id)
		}
		if selected, ok := parseOptionalBool(r.URL.Query().Get("selected")); ok {
			q = q.Where("selected = ?", selected)
		}
		var models []db.Dns
		if err := q.Find(&models).Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		expandParsed := shouldExpandParsed(r)
		items := make([]dnsResource, 0, len(models))
		for _, model := range models {
			item := buildDNSResourceRaw(&model)
			if expandParsed {
				var err error
				item, err = buildDNSResource(&model)
				if err != nil {
					writeError(rw, http.StatusInternalServerError, err.Error())
					return
				}
			}
			items = append(items, item)
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var req dnsMutationRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}

		model := db.Dns{
			Name: optionalString(req.Name),
			Dns:  normalizeEnsureSection("dns", optionalString(req.DNS), engine.Default().EmptyDnsSection()),
		}
		if _, err := engine.Default().ParseConfig(nil, &model.Dns, nil); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if err := db.DB(r.Context()).Create(&model).Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		item, err := buildDNSResource(&model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, item)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost)
	}
}

func handleDNSResource(rw http.ResponseWriter, r *http.Request) {
	if _, ok := parseResourceSelectionPath(r.URL.Path, "dns"); ok {
		handleDNSSelection(rw, r)
		return
	}
	id, ok := parseResourcePath(r.URL.Path, "dns")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		model, err := getDNSModel(db.DB(r.Context()), id)
		if err != nil {
			writeModelError(rw, err)
			return
		}
		item, err := buildDNSResource(model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, item)
	case http.MethodPut:
		var req dnsMutationRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if req.Name == nil && req.DNS == nil {
			writeError(rw, http.StatusBadRequest, "at least one field must be provided")
			return
		}
		tx := db.BeginTx(r.Context())
		model, err := getDNSModel(tx, id)
		if err != nil {
			tx.Rollback()
			writeModelError(rw, err)
			return
		}
		updates := map[string]any{}
		if req.Name != nil {
			updates["name"] = *req.Name
		}
		if req.DNS != nil {
			dnsSection := normalizeEnsureSection("dns", *req.DNS, engine.Default().EmptyDnsSection())
			if _, err := engine.Default().ParseConfig(nil, &dnsSection, nil); err != nil {
				tx.Rollback()
				writeError(rw, http.StatusBadRequest, err.Error())
				return
			}
			updates["dns"] = dnsSection
			updates["version"] = gorm.Expr("version + 1")
		}
		if err := tx.Model(&db.Dns{ID: id}).Updates(updates).Error; err != nil {
			tx.Rollback()
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		model, err = getDNSModel(tx, id)
		if err != nil {
			tx.Rollback()
			writeModelError(rw, err)
			return
		}
		if err := tx.Commit().Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		item, err := buildDNSResource(model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, item)
	case http.MethodDelete:
		q := db.DB(r.Context()).Delete(&db.Dns{}, id)
		if q.Error != nil {
			writeError(rw, http.StatusInternalServerError, q.Error.Error())
			return
		}
		if q.RowsAffected == 0 {
			writeError(rw, http.StatusNotFound, "no such dns")
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
	}
}

func handleDNSSelection(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	id, ok := parseResourceSelectionPath(r.URL.Path, "dns")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}
	applied, err := orchestrator.SelectDNS(r.Context(), id)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"applied": applied, "selectedId": id})
}

func handleRoutings(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := db.DB(r.Context()).Model(&db.Routing{}).Order("id asc")
		if id, ok := parseOptionalUint(r.URL.Query().Get("id")); ok {
			q = q.Where("id = ?", id)
		}
		if selected, ok := parseOptionalBool(r.URL.Query().Get("selected")); ok {
			q = q.Where("selected = ?", selected)
		}
		var models []db.Routing
		if err := q.Find(&models).Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		expandParsed := shouldExpandParsed(r)
		items := make([]routingResource, 0, len(models))
		for _, model := range models {
			item := buildRoutingResourceRaw(&model)
			if expandParsed {
				var err error
				item, err = buildRoutingResource(&model)
				if err != nil {
					writeError(rw, http.StatusInternalServerError, err.Error())
					return
				}
			}
			items = append(items, item)
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var req routingMutationRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}

		model := db.Routing{
			Name:    optionalString(req.Name),
			Routing: normalizeEnsureSection("routing", optionalString(req.Routing), engine.Default().EmptyRoutingSection()),
		}
		if _, err := engine.Default().ParseConfig(nil, nil, &model.Routing); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if err := db.DB(r.Context()).Create(&model).Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		item, err := buildRoutingResource(&model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, item)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost)
	}
}

func handleRoutingResource(rw http.ResponseWriter, r *http.Request) {
	if _, ok := parseResourceSelectionPath(r.URL.Path, "routings"); ok {
		handleRoutingSelection(rw, r)
		return
	}
	id, ok := parseResourcePath(r.URL.Path, "routings")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		model, err := getRoutingModel(db.DB(r.Context()), id)
		if err != nil {
			writeModelError(rw, err)
			return
		}
		item, err := buildRoutingResource(model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, item)
	case http.MethodPut:
		var req routingMutationRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if req.Name == nil && req.Routing == nil {
			writeError(rw, http.StatusBadRequest, "at least one field must be provided")
			return
		}
		tx := db.BeginTx(r.Context())
		model, err := getRoutingModel(tx, id)
		if err != nil {
			tx.Rollback()
			writeModelError(rw, err)
			return
		}
		updates := map[string]any{}
		if req.Name != nil {
			updates["name"] = *req.Name
		}
		if req.Routing != nil {
			routingSection := normalizeEnsureSection("routing", *req.Routing, engine.Default().EmptyRoutingSection())
			if _, err := engine.Default().ParseConfig(nil, nil, &routingSection); err != nil {
				tx.Rollback()
				writeError(rw, http.StatusBadRequest, err.Error())
				return
			}
			updates["routing"] = routingSection
			updates["version"] = gorm.Expr("version + 1")
		}
		if err := tx.Model(&db.Routing{ID: id}).Updates(updates).Error; err != nil {
			tx.Rollback()
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		model, err = getRoutingModel(tx, id)
		if err != nil {
			tx.Rollback()
			writeModelError(rw, err)
			return
		}
		if err := tx.Commit().Error; err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		item, err := buildRoutingResource(model)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, item)
	case http.MethodDelete:
		q := db.DB(r.Context()).Delete(&db.Routing{}, id)
		if q.Error != nil {
			writeError(rw, http.StatusInternalServerError, q.Error.Error())
			return
		}
		if q.RowsAffected == 0 {
			writeError(rw, http.StatusNotFound, "no such routing")
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
	}
}

func handleRoutingSelection(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	id, ok := parseResourceSelectionPath(r.URL.Path, "routings")
	if !ok {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}
	applied, err := orchestrator.SelectRouting(r.Context(), id)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"applied": applied, "selectedId": id})
}

func parseOptionalBool(raw string) (bool, bool) {
	if raw == "" {
		return false, false
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	default:
		return false, false
	}
}

func parseResourcePath(path string, resource string) (uint, bool) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] != resource {
		return 0, false
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

func parseResourceSelectionPath(path string, resource string) (uint, bool) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 || parts[0] != resource || parts[2] != "select" {
		return 0, false
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

func optionalString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func normalizeSection(section *string, fallback string) string {
	if section == nil {
		return fallback
	}
	if strings.TrimSpace(*section) == "" {
		return fallback
	}
	return *section
}

func shouldExpandParsed(r *http.Request) bool {
	for _, item := range strings.Split(r.URL.Query().Get("expand"), ",") {
		switch strings.TrimSpace(strings.ToLower(item)) {
		case "parsed", "all":
			return true
		}
	}
	return false
}

func getConfigModel(d *gorm.DB, id uint) (*db.Config, error) {
	var model db.Config
	if err := d.First(&model, id).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func getDNSModel(d *gorm.DB, id uint) (*db.Dns, error) {
	var model db.Dns
	if err := d.First(&model, id).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func getRoutingModel(d *gorm.DB, id uint) (*db.Routing, error) {
	var model db.Routing
	if err := d.First(&model, id).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func writeModelError(rw http.ResponseWriter, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(rw, http.StatusNotFound, "not found")
		return
	}
	writeError(rw, http.StatusInternalServerError, err.Error())
}
