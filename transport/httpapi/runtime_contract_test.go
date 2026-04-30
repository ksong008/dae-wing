package httpapi

import (
	"net/http/httptest"
	"testing"
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
