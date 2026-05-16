/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/orchestrator"
)

const (
	defaultOverviewWindowSec = 60
	defaultOverviewMaxPoints = 16
	maxOverviewWindowSec     = 60 * 60
	maxOverviewMaxPoints     = 240
	maxListLimit             = 200
	maxJSONBodyBytes         = 1 << 20
	defaultReloadTimeout     = 30 * time.Second
	defaultStopTimeout       = 10 * time.Second
	maxRuntimeTimeout        = 120 * time.Second
	streamHeartbeatInterval  = 15 * time.Second
)

type runtimeOverviewResponse struct {
	UpdatedAt             string                         `json:"updatedAt"`
	UploadRate            string                         `json:"uploadRate"`
	DownloadRate          string                         `json:"downloadRate"`
	UploadTotal           string                         `json:"uploadTotal"`
	DownloadTotal         string                         `json:"downloadTotal"`
	ActiveConnections     int                            `json:"activeConnections"`
	UDPSessions           int                            `json:"udpSessions"`
	UDPTaskQueues         int                            `json:"udpTaskQueues"`
	UDPTaskDropTotal      string                         `json:"udpTaskDropTotal"`
	PacketSnifferSessions int                            `json:"packetSnifferSessions"`
	RSSBytes              string                         `json:"rssBytes"`
	HeapAllocBytes        string                         `json:"heapAllocBytes"`
	Goroutines            int                            `json:"goroutines"`
	Samples               []runtimeTrafficSampleResponse `json:"samples"`
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
	Dry        bool `json:"dry"`
	TimeoutSec int  `json:"timeoutSec"`
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
	mux.HandleFunc("/general/cache-stats", requireAuth(handleGeneralCacheStats))
	mux.HandleFunc("/openapi.json", handleOpenAPI)
	mux.HandleFunc("/configs", requireAuth(handleConfigs))
	mux.HandleFunc("/configs/parsed", requireAuth(handleParsedConfig))
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
	mux.HandleFunc("/user/me/dae-config-file", requireAuth(handleCurrentUserDAEConfigFile))
	mux.HandleFunc("/user/me/dae-config-file/preview", requireAuth(handleCurrentUserDAEConfigFilePreview))
	mux.HandleFunc("/user/me/dae-bundle", requireAuth(handleCurrentUserDAEBundle))
	mux.HandleFunc("/user/me/default-resources", requireAuth(handleCurrentUserDefaultResources))
	mux.HandleFunc("/user/me/password", requireAuth(handleCurrentUserPassword))
	mux.HandleFunc("/user/me/storage", requireAuth(handleCurrentUserStorage))
	mux.HandleFunc("/runtime/overview", requireAuth(handleRuntimeOverview))
	mux.HandleFunc("/runtime/log-level", requireAuth(handleRuntimeLogLevel))
	mux.HandleFunc("/runtime/reload", requireAuth(handleRuntimeReload))
	mux.HandleFunc("/runtime/stop", requireAuth(handleRuntimeStop))
	mux.HandleFunc("/events/runtime", requireAuth(handleRuntimeEvents))
	mux.HandleFunc("/events/logs", requireAuth(handleLogEvents))
	mux.HandleFunc("/logs", requireAuth(handleLogs))
	mux.HandleFunc("/logs/settings", requireAuth(handleLogSettings))
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

	timeout, err := runtimeOperationTimeout(req.TimeoutSec, defaultReloadTimeout)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	applied, err := orchestrator.Run(ctx, req.Dry)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
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
	timeout, err := runtimeOperationTimeout(req.TimeoutSec, defaultStopTimeout)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	if err := orchestrator.Stop(r.Context(), timeout); err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, stopResponse{Stopped: true})
}

func runtimeOperationTimeout(timeoutSec int, fallback time.Duration) (time.Duration, error) {
	if timeoutSec < 0 {
		return 0, fmt.Errorf("timeoutSec must not be negative")
	}
	if timeoutSec == 0 {
		return fallback, nil
	}
	maxTimeoutSec := int(maxRuntimeTimeout / time.Second)
	if timeoutSec > maxTimeoutSec {
		return 0, fmt.Errorf("timeoutSec must not exceed %d", int(maxRuntimeTimeout/time.Second))
	}
	return time.Duration(timeoutSec) * time.Second, nil
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
	disableResponseWriteDeadline(rw)

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("X-Accel-Buffering", "no")
	rw.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(rw, "retry: 3000\n\n")
	flusher.Flush()

	lastSampleTimestamp := time.Time{}

	sendOverviewEvent := func(event string, payload runtimeOverviewResponse) bool {
		return writeSSE(rw, flusher, event, payload)
	}

	sendFullOverviewEvent := func() bool {
		overview, err := engine.Default().GetRuntimeOverview(windowSec, maxPoints)
		if err != nil {
			return writeSSE(rw, flusher, "runtime.error", map[string]string{"error": err.Error()})
		}
		payload := runtimeOverviewFromModel(overview)
		lastSampleTimestamp = runtimeOverviewLastSampleTimestamp(overview)
		return sendOverviewEvent("runtime.overview", payload)
	}

	sendDeltaOverviewEvent := func() bool {
		overview, err := engine.Default().GetRuntimeOverview(windowSec, maxPoints)
		if err != nil {
			return writeSSE(rw, flusher, "runtime.error", map[string]string{"error": err.Error()})
		}
		payload, nextLastSampleTimestamp := runtimeOverviewDeltaFromModel(overview, lastSampleTimestamp)
		lastSampleTimestamp = nextLastSampleTimestamp
		return sendOverviewEvent("runtime.overview.delta", payload)
	}

	if !sendFullOverviewEvent() {
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
			if !sendDeltaOverviewEvent() {
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
	return parsePositiveIntClamped(r.URL.Query().Get("windowSec"), defaultOverviewWindowSec, maxOverviewWindowSec),
		parsePositiveIntClamped(r.URL.Query().Get("maxPoints"), defaultOverviewMaxPoints, maxOverviewMaxPoints)
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
		UpdatedAt:             overview.UpdatedAt.Format(time.RFC3339Nano),
		UploadRate:            strconv.FormatUint(overview.UploadRate, 10),
		DownloadRate:          strconv.FormatUint(overview.DownloadRate, 10),
		UploadTotal:           strconv.FormatUint(overview.UploadTotal, 10),
		DownloadTotal:         strconv.FormatUint(overview.DownloadTotal, 10),
		ActiveConnections:     overview.ActiveConnections,
		UDPSessions:           overview.UDPSessions,
		UDPTaskQueues:         overview.UDPTaskQueues,
		UDPTaskDropTotal:      strconv.FormatUint(overview.UDPTaskDropTotal, 10),
		PacketSnifferSessions: overview.PacketSnifferSessions,
		RSSBytes:              strconv.FormatUint(overview.RSSBytes, 10),
		HeapAllocBytes:        strconv.FormatUint(overview.HeapAllocBytes, 10),
		Goroutines:            overview.Goroutines,
		Samples:               samples,
	}
}

func runtimeOverviewDeltaFromModel(overview *engine.RuntimeOverview, after time.Time) (runtimeOverviewResponse, time.Time) {
	samples := make([]runtimeTrafficSampleResponse, 0, len(overview.Samples))
	lastSampleTimestamp := after
	for _, sample := range overview.Samples {
		if !sample.Timestamp.After(after) {
			continue
		}
		samples = append(samples, runtimeTrafficSampleResponse{
			Timestamp:    sample.Timestamp.Format(time.RFC3339Nano),
			UploadRate:   strconv.FormatUint(sample.UploadRate, 10),
			DownloadRate: strconv.FormatUint(sample.DownloadRate, 10),
		})
		lastSampleTimestamp = sample.Timestamp
	}
	payload := runtimeOverviewResponse{
		UpdatedAt:             overview.UpdatedAt.Format(time.RFC3339Nano),
		UploadRate:            strconv.FormatUint(overview.UploadRate, 10),
		DownloadRate:          strconv.FormatUint(overview.DownloadRate, 10),
		UploadTotal:           strconv.FormatUint(overview.UploadTotal, 10),
		DownloadTotal:         strconv.FormatUint(overview.DownloadTotal, 10),
		ActiveConnections:     overview.ActiveConnections,
		UDPSessions:           overview.UDPSessions,
		UDPTaskQueues:         overview.UDPTaskQueues,
		UDPTaskDropTotal:      strconv.FormatUint(overview.UDPTaskDropTotal, 10),
		PacketSnifferSessions: overview.PacketSnifferSessions,
		RSSBytes:              strconv.FormatUint(overview.RSSBytes, 10),
		HeapAllocBytes:        strconv.FormatUint(overview.HeapAllocBytes, 10),
		Goroutines:            overview.Goroutines,
		Samples:               samples,
	}
	return payload, lastSampleTimestamp
}

func runtimeOverviewLastSampleTimestamp(overview *engine.RuntimeOverview) time.Time {
	if len(overview.Samples) == 0 {
		return time.Time{}
	}
	return overview.Samples[len(overview.Samples)-1].Timestamp
}

func decodeJSONBody(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read json body: %w", err)
	}
	if len(body) > maxJSONBodyBytes {
		return fmt.Errorf("json body exceeds %d bytes", maxJSONBodyBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("invalid json body: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid json body: multiple json values")
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

func parsePositiveIntClamped(raw string, fallback int, maxValue int) int {
	parsed := parsePositiveInt(raw, fallback)
	if maxValue > 0 && parsed > maxValue {
		return maxValue
	}
	return parsed
}

func parseListLimit(raw string) int {
	return parsePositiveIntClamped(raw, 0, maxListLimit)
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

func disableResponseWriteDeadline(rw http.ResponseWriter) {
	_ = http.NewResponseController(rw).SetWriteDeadline(time.Time{})
}
