/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"net/http"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/orchestrator"
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
