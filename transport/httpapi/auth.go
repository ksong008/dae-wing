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

type tokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleAuthToken(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	var req tokenRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	token, err := orchestrator.IssueToken(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(rw, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"token": token})
}

func handleAuthStatus(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	numberUsers, err := orchestrator.NumberUsers(r.Context())
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"numberUsers": numberUsers})
}

func handleAuthUsers(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	var req createUserRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	token, err := orchestrator.CreateUser(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusCreated, map[string]any{"token": token})
}

func currentUserFromContext(r *http.Request) (*db.User, bool) {
	userValue := r.Context().Value("user")
	if userValue == nil {
		return nil, false
	}
	user, ok := userValue.(*db.User)
	return user, ok
}
