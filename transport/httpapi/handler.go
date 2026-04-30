/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/orchestrator"
)

const (
	defaultOverviewWindowSec = 60
	defaultOverviewMaxPoints = 16
	defaultStopTimeout       = 10 * time.Second
	streamHeartbeatInterval  = 15 * time.Second
)

type runtimeOverviewResponse struct {
	UpdatedAt         string                         `json:"updatedAt"`
	UploadRate        string                         `json:"uploadRate"`
	DownloadRate      string                         `json:"downloadRate"`
	UploadTotal       string                         `json:"uploadTotal"`
	DownloadTotal     string                         `json:"downloadTotal"`
	ActiveConnections int                            `json:"activeConnections"`
	UDPSessions       int                            `json:"udpSessions"`
	RSSBytes          string                         `json:"rssBytes"`
	HeapAllocBytes    string                         `json:"heapAllocBytes"`
	Goroutines        int                            `json:"goroutines"`
	Samples           []runtimeTrafficSampleResponse `json:"samples"`
}

type runtimeTrafficSampleResponse struct {
	Timestamp    string `json:"timestamp"`
	UploadRate   string `json:"uploadRate"`
	DownloadRate string `json:"downloadRate"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type reloadRequest struct {
	Dry bool `json:"dry"`
}

type reloadResponse struct {
	Applied int32 `json:"applied"`
	Dry     bool  `json:"dry"`
}

type stopRequest struct {
	TimeoutSec int `json:"timeoutSec"`
}

type stopResponse struct {
	Stopped bool `json:"stopped"`
}

func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/auth/status", handleAuthStatus)
	mux.HandleFunc("/auth/token", handleAuthToken)
	mux.HandleFunc("/auth/users", handleAuthUsers)
	mux.HandleFunc("/general/interfaces", requireAuth(handleGeneralInterfaces))
	mux.HandleFunc("/general/state", requireAuth(handleGeneralState))
	mux.HandleFunc("/openapi.json", handleOpenAPI)
	mux.HandleFunc("/configs", requireAuth(handleConfigs))
	mux.HandleFunc("/configs/", requireAuth(handleConfigResource))
	mux.HandleFunc("/configs/flat-desc", requireAuth(handleConfigFlatDesc))
	mux.HandleFunc("/dns", requireAuth(handleDNSResources))
	mux.HandleFunc("/dns/parsed", requireAuth(handleParsedDNS))
	mux.HandleFunc("/dns/", requireAuth(handleDNSResource))
	mux.HandleFunc("/groups", requireAuth(handleGroups))
	mux.HandleFunc("/groups/", requireAuth(handleGroupResource))
	mux.HandleFunc("/nodes", requireAuth(handleNodes))
	mux.HandleFunc("/nodes/", requireAuth(handleNodeResource))
	mux.HandleFunc("/routings", requireAuth(handleRoutings))
	mux.HandleFunc("/routings/parsed", requireAuth(handleParsedRouting))
	mux.HandleFunc("/routings/", requireAuth(handleRoutingResource))
	mux.HandleFunc("/subscriptions", requireAuth(handleSubscriptions))
	mux.HandleFunc("/subscriptions/", requireAuth(handleSubscriptionResource))
	mux.HandleFunc("/user/me", requireAuth(handleCurrentUser))
	mux.HandleFunc("/user/me/default-resources", requireAuth(handleCurrentUserDefaultResources))
	mux.HandleFunc("/user/me/password", requireAuth(handleCurrentUserPassword))
	mux.HandleFunc("/user/me/storage", requireAuth(handleCurrentUserStorage))
	mux.HandleFunc("/runtime/overview", requireAuth(handleRuntimeOverview))
	mux.HandleFunc("/runtime/reload", requireAuth(handleRuntimeReload))
	mux.HandleFunc("/runtime/stop", requireAuth(handleRuntimeStop))
	mux.HandleFunc("/events/runtime", requireAuth(handleRuntimeEvents))
	return mux
}

func handleHealth(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"healthCheck": 1})
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if r.Context().Value("user") == nil {
			writeError(rw, http.StatusUnauthorized, "authentication required")
			return
		}
		next(rw, r)
	}
}

func handleRuntimeOverview(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	windowSec, maxPoints := runtimeOverviewQueryValues(r)

	overview, err := engine.Default().GetRuntimeOverview(windowSec, maxPoints)
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, runtimeOverviewFromModel(overview))
}

func handleRuntimeReload(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	var req reloadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}

	tx := db.BeginTx(r.Context())
	applied, err := orchestrator.Run(tx, req.Dry)
	if err != nil {
		tx.Rollback()
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	tx.Commit()
	writeJSON(rw, http.StatusOK, reloadResponse{
		Applied: applied,
		Dry:     req.Dry,
	})
}

func handleRuntimeStop(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}
	var req stopRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	timeout := defaultStopTimeout
	if req.TimeoutSec > 0 {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}
	if err := orchestrator.Stop(r.Context(), timeout); err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, stopResponse{Stopped: true})
}

func handleRuntimeEvents(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	windowSec, maxPoints := runtimeOverviewQueryValues(r)

	flusher, ok := rw.(http.Flusher)
	if !ok {
		writeError(rw, http.StatusInternalServerError, "streaming is not supported by this response writer")
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("X-Accel-Buffering", "no")
	rw.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(rw, "retry: 3000\n\n")
	flusher.Flush()

	sendOverviewEvent := func() bool {
		overview, err := engine.Default().GetRuntimeOverview(windowSec, maxPoints)
		if err != nil {
			return writeSSE(rw, flusher, "runtime.error", map[string]string{"error": err.Error()})
		}
		return writeSSE(rw, flusher, "runtime.overview", runtimeOverviewFromModel(overview))
	}

	if !sendOverviewEvent() {
		return
	}

	streamTicker := time.NewTicker(runtimeOverviewStreamInterval(windowSec))
	defer streamTicker.Stop()
	heartbeatTicker := time.NewTicker(streamHeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-streamTicker.C:
			if !sendOverviewEvent() {
				return
			}
		case <-heartbeatTicker.C:
			if _, err := io.WriteString(rw, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func runtimeOverviewQueryValues(r *http.Request) (windowSec int, maxPoints int) {
	return parsePositiveInt(r.URL.Query().Get("windowSec"), defaultOverviewWindowSec),
		parsePositiveInt(r.URL.Query().Get("maxPoints"), defaultOverviewMaxPoints)
}

func runtimeOverviewStreamInterval(windowSec int) time.Duration {
	if windowSec <= 60 {
		return time.Second
	}
	if windowSec <= 10*60 {
		return 2 * time.Second
	}
	if windowSec <= 30*60 {
		return 5 * time.Second
	}
	return 10 * time.Second
}

func runtimeOverviewFromModel(overview *engine.RuntimeOverview) runtimeOverviewResponse {
	samples := make([]runtimeTrafficSampleResponse, 0, len(overview.Samples))
	for _, sample := range overview.Samples {
		samples = append(samples, runtimeTrafficSampleResponse{
			Timestamp:    sample.Timestamp.Format(time.RFC3339Nano),
			UploadRate:   strconv.FormatUint(sample.UploadRate, 10),
			DownloadRate: strconv.FormatUint(sample.DownloadRate, 10),
		})
	}
	return runtimeOverviewResponse{
		UpdatedAt:         overview.UpdatedAt.Format(time.RFC3339Nano),
		UploadRate:        strconv.FormatUint(overview.UploadRate, 10),
		DownloadRate:      strconv.FormatUint(overview.DownloadRate, 10),
		UploadTotal:       strconv.FormatUint(overview.UploadTotal, 10),
		DownloadTotal:     strconv.FormatUint(overview.DownloadTotal, 10),
		ActiveConnections: overview.ActiveConnections,
		UDPSessions:       overview.UDPSessions,
		RSSBytes:          strconv.FormatUint(overview.RSSBytes, 10),
		HeapAllocBytes:    strconv.FormatUint(overview.HeapAllocBytes, 10),
		Goroutines:        overview.Goroutines,
		Samples:           samples,
	}
}

func decodeJSONBody(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("invalid json body: %w", err)
	}
	return nil
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func writeJSON(rw http.ResponseWriter, status int, payload any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(payload)
}

func writeError(rw http.ResponseWriter, status int, message string) {
	writeJSON(rw, status, errorResponse{Error: message})
}

func writeMethodNotAllowed(rw http.ResponseWriter, allow string) {
	rw.Header().Set("Allow", allow)
	writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
}

func writeSSE(rw http.ResponseWriter, flusher http.Flusher, event string, payload any) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	if _, err = fmt.Fprintf(rw, "event: %s\n", event); err != nil {
		return false
	}
	if _, err = fmt.Fprintf(rw, "data: %s\n\n", data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
