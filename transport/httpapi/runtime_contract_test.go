package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daeuniverse/dae-wing/engine"
)

func TestRuntimeOverviewQueryValues(t *testing.T) {
	t.Run("uses explicit query values", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/events/runtime?windowSec=600&maxPoints=240", nil)
		windowSec, maxPoints := runtimeOverviewQueryValues(req)
		if windowSec != 600 || maxPoints != 240 {
			t.Fatalf("runtimeOverviewQueryValues = (%d, %d), want (600, 240)", windowSec, maxPoints)
		}
	})

	t.Run("falls back on invalid values", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/events/runtime?windowSec=0&maxPoints=bad", nil)
		windowSec, maxPoints := runtimeOverviewQueryValues(req)
		if windowSec != defaultOverviewWindowSec || maxPoints != defaultOverviewMaxPoints {
			t.Fatalf("runtimeOverviewQueryValues = (%d, %d), want defaults (%d, %d)", windowSec, maxPoints, defaultOverviewWindowSec, defaultOverviewMaxPoints)
		}
	})

	t.Run("clamps excessive values", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/events/runtime?windowSec=999999&maxPoints=999999", nil)
		windowSec, maxPoints := runtimeOverviewQueryValues(req)
		if windowSec != maxOverviewWindowSec || maxPoints != maxOverviewMaxPoints {
			t.Fatalf("runtimeOverviewQueryValues = (%d, %d), want max (%d, %d)", windowSec, maxPoints, maxOverviewWindowSec, maxOverviewMaxPoints)
		}
	})
}

func TestParseListLimitClampsExcessiveValues(t *testing.T) {
	if got := parseListLimit("999999"); got != maxListLimit {
		t.Fatalf("parseListLimit() = %d, want %d", got, maxListLimit)
	}
	if got := parseListLimit("bad"); got != 0 {
		t.Fatalf("parseListLimit(bad) = %d, want 0", got)
	}
}

func TestDecodeJSONBodyRejectsTrailingJSONValues(t *testing.T) {
	req := httptest.NewRequest("POST", "/runtime/reload", strings.NewReader(`{"dry":false} {"dry":true}`))
	var dst reloadRequest
	if err := decodeJSONBody(req, &dst); err == nil {
		t.Fatal("decodeJSONBody() accepted multiple json values")
	}
}

func TestDecodeJSONBodyRejectsOversizeBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/runtime/reload", strings.NewReader(strings.Repeat("a", maxJSONBodyBytes+1)))
	var dst reloadRequest
	if err := decodeJSONBody(req, &dst); err == nil {
		t.Fatal("decodeJSONBody() accepted an oversized body")
	}
}

func TestRuntimeOperationTimeout(t *testing.T) {
	t.Run("uses fallback", func(t *testing.T) {
		timeout, err := runtimeOperationTimeout(0, 7*time.Second)
		if err != nil {
			t.Fatalf("runtimeOperationTimeout() error = %v", err)
		}
		if timeout != 7*time.Second {
			t.Fatalf("runtimeOperationTimeout() = %v, want 7s", timeout)
		}
	})

	t.Run("rejects negative", func(t *testing.T) {
		if _, err := runtimeOperationTimeout(-1, time.Second); err == nil {
			t.Fatal("runtimeOperationTimeout() accepted a negative timeout")
		}
	})

	t.Run("rejects excessive", func(t *testing.T) {
		if _, err := runtimeOperationTimeout(int(maxRuntimeTimeout/time.Second)+1, time.Second); err == nil {
			t.Fatal("runtimeOperationTimeout() accepted an excessive timeout")
		}
	})
}

func TestDisableResponseWriteDeadlineClearsDeadline(t *testing.T) {
	rec := &writeDeadlineRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		deadline:         time.Now(),
	}

	disableResponseWriteDeadline(rec)

	if !rec.called {
		t.Fatal("SetWriteDeadline was not called")
	}
	if !rec.deadline.IsZero() {
		t.Fatalf("deadline = %v, want zero time", rec.deadline)
	}

	disableResponseWriteDeadline(httptest.NewRecorder())
}

type writeDeadlineRecorder struct {
	*httptest.ResponseRecorder
	called   bool
	deadline time.Time
}

func (r *writeDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.called = true
	r.deadline = deadline
	return nil
}

func TestRuntimeEventsOpenAPIIncludesOverviewQueryParameters(t *testing.T) {
	doc := OpenAPIDocument()
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths = %#v", doc["paths"])
	}
	eventPath, ok := paths["/api/events/runtime"].(map[string]any)
	if !ok {
		t.Fatalf("events path = %#v", paths["/api/events/runtime"])
	}
	getOp, ok := eventPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("events get op = %#v", eventPath["get"])
	}
	params, ok := getOp["parameters"].([]map[string]any)
	if !ok {
		t.Fatalf("events parameters = %#v", getOp["parameters"])
	}
	if len(params) != 3 {
		t.Fatalf("events parameters len = %d, want 3", len(params))
	}
	if params[0]["name"] != "windowSec" || params[1]["name"] != "maxPoints" || params[2]["name"] != "access_token" {
		t.Fatalf("events parameter names = %#v", params)
	}
}

func TestRuntimeOverviewStreamInterval(t *testing.T) {
	tests := []struct {
		name      string
		windowSec int
		want      time.Duration
	}{
		{name: "1m uses 1s", windowSec: 60, want: time.Second},
		{name: "10m uses 2s", windowSec: 10 * 60, want: 2 * time.Second},
		{name: "30m uses 5s", windowSec: 30 * 60, want: 5 * time.Second},
		{name: "1h uses 10s", windowSec: 60 * 60, want: 10 * time.Second},
		{name: "invalid falls back to 1s", windowSec: 0, want: time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeOverviewStreamInterval(tt.windowSec); got != tt.want {
				t.Fatalf("runtimeOverviewStreamInterval(%d) = %v, want %v", tt.windowSec, got, tt.want)
			}
		})
	}
}

func TestRuntimeOverviewDeltaFromModel(t *testing.T) {
	overview := &engine.RuntimeOverview{
		UpdatedAt:             time.Unix(1_700_000_100, 0),
		UploadRate:            11,
		DownloadRate:          22,
		UploadTotal:           33,
		DownloadTotal:         44,
		ActiveConnections:     5,
		UDPSessions:           6,
		UDPTaskQueues:         7,
		UDPTaskDropTotal:      8,
		PacketSnifferSessions: 9,
		RSSBytes:              77,
		HeapAllocBytes:        88,
		Goroutines:            10,
		Samples: []engine.RuntimeTrafficSample{
			{Timestamp: time.Unix(1_700_000_000, 0), UploadRate: 1, DownloadRate: 2},
			{Timestamp: time.Unix(1_700_000_050, 0), UploadRate: 3, DownloadRate: 4},
			{Timestamp: time.Unix(1_700_000_090, 0), UploadRate: 5, DownloadRate: 6},
		},
	}

	delta, next := runtimeOverviewDeltaFromModel(overview, time.Unix(1_700_000_050, 0))
	if len(delta.Samples) != 1 {
		t.Fatalf("delta samples len = %d, want 1", len(delta.Samples))
	}
	if delta.Samples[0].UploadRate != "5" || delta.Samples[0].DownloadRate != "6" {
		t.Fatalf("delta sample = %+v", delta.Samples[0])
	}
	if next.Unix() != 1_700_000_090 {
		t.Fatalf("next sample timestamp = %v, want %v", next, time.Unix(1_700_000_090, 0))
	}
	if delta.UploadRate != "11" || delta.DownloadRate != "22" {
		t.Fatalf("unexpected scalar fields in delta: %+v", delta)
	}
	if delta.UDPTaskQueues != 7 || delta.UDPTaskDropTotal != "8" || delta.PacketSnifferSessions != 9 {
		t.Fatalf("unexpected udp telemetry fields in delta: %+v", delta)
	}
}
