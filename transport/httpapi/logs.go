package httpapi

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/logstore"
	"github.com/sirupsen/logrus"
)

type logSettingsPatchRequest struct {
	MaxEntries *int   `json:"maxEntries"`
	MaxBytes   *int64 `json:"maxBytes"`
}

type runtimeLogLevelRequest struct {
	Level string `json:"level"`
}

type runtimeLogLevelResponse struct {
	Level string `json:"level"`
}

func handleLogs(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		level, err := parseLogLevelFilter(r.URL.Query().Get("level"))
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 0)
		entries, err := logstore.Default().Query(logstore.QueryFilter{
			Level: level,
			Query: r.URL.Query().Get("q"),
			Limit: limit,
		})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"items": entries})
	case http.MethodDelete:
		if err := logstore.Default().Clear(); err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"cleared": true})
	default:
		writeMethodNotAllowed(rw, strings.Join([]string{http.MethodGet, http.MethodDelete}, ", "))
	}
}

func handleLogSettings(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeLogSettings(rw, logstore.Default().Settings())
	case http.MethodPatch:
		var req logSettingsPatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		settings := logstore.Default().Settings()
		if req.MaxEntries != nil {
			settings.MaxEntries = *req.MaxEntries
		}
		if req.MaxBytes != nil {
			settings.MaxBytes = *req.MaxBytes
		}
		updated, err := logstore.Default().SetSettings(settings)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeLogSettings(rw, updated)
	default:
		writeMethodNotAllowed(rw, strings.Join([]string{http.MethodGet, http.MethodPatch}, ", "))
	}
}

func handleRuntimeLogLevel(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(rw, http.StatusOK, runtimeLogLevelResponse{Level: logstore.LevelName(logrus.GetLevel())})
	case http.MethodPatch:
		var req runtimeLogLevelRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		level, err := logrus.ParseLevel(strings.TrimSpace(req.Level))
		if err != nil {
			writeError(rw, http.StatusBadRequest, "invalid log level")
			return
		}
		engine.Default().SetLogLevel(level)
		writeJSON(rw, http.StatusOK, runtimeLogLevelResponse{Level: logstore.LevelName(level)})
	default:
		writeMethodNotAllowed(rw, strings.Join([]string{http.MethodGet, http.MethodPatch}, ", "))
	}
}

func handleLogEvents(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}
	level, err := parseLogLevelFilter(r.URL.Query().Get("level"))
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

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

	entries, unsubscribe := logstore.Default().Subscribe(100)
	defer unsubscribe()

	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(rw, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case entry, ok := <-entries:
			if !ok {
				return
			}
			if !logEntryMatchesLiveFilter(entry, level, query) {
				continue
			}
			if !writeSSE(rw, flusher, "log.entry", entry) {
				return
			}
		}
	}
}

func writeLogSettings(rw http.ResponseWriter, settings logstore.Settings) {
	writeJSON(rw, http.StatusOK, map[string]any{
		"maxEntries":    settings.MaxEntries,
		"maxBytes":      settings.MaxBytes,
		"minMaxEntries": logstore.MinMaxEntries,
		"maxMaxEntries": logstore.MaxMaxEntries,
		"minMaxBytes":   logstore.MinMaxBytes,
		"maxMaxBytes":   logstore.MaxMaxBytes,
	})
}

func parseLogLevelFilter(raw string) (string, error) {
	level := strings.ToLower(strings.TrimSpace(raw))
	if level == "" || level == "all" {
		return level, nil
	}
	parsed, err := logrus.ParseLevel(level)
	if err != nil {
		return "", err
	}
	return logstore.LevelName(parsed), nil
}

func logEntryMatchesLiveFilter(entry logstore.Entry, level string, query string) bool {
	if level != "" && level != "all" && logstore.CanonicalLevelName(entry.Level) != level {
		return false
	}
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Message), query) {
		return true
	}
	for key, value := range entry.Fields {
		if strings.Contains(strings.ToLower(key), query) || strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
