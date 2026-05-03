package httpapi

import (
	"net/http/httptest"
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
		UpdatedAt:         time.Unix(1_700_000_100, 0),
		UploadRate:        11,
		DownloadRate:      22,
		UploadTotal:       33,
		DownloadTotal:     44,
		ActiveConnections: 5,
		UDPSessions:       6,
		RSSBytes:          77,
		HeapAllocBytes:    88,
		Goroutines:        9,
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
}
