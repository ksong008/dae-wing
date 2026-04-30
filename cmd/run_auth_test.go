package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/orchestrator"
)

type requestContextKey struct{}

func TestRequestAuthToken(t *testing.T) {
	t.Run("prefers bearer authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, runtimeEventsAPIPath+"?access_token=query-token", nil)
		req.Header.Set("Authorization", "Bearer header-token")

		if token := requestAuthToken(req); token != "header-token" {
			t.Fatalf("requestAuthToken() = %q, want header-token", token)
		}
	})

	t.Run("accepts query token for runtime event stream only", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, runtimeEventsAPIPath+"?access_token=query-token", nil)

		if token := requestAuthToken(req); token != "query-token" {
			t.Fatalf("requestAuthToken() = %q, want query-token", token)
		}
	})

	t.Run("rejects query token on non-streaming paths", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/runtime/overview?access_token=query-token", nil)

		if token := requestAuthToken(req); token != "" {
			t.Fatalf("requestAuthToken() = %q, want empty token", token)
		}
	})
}

func TestAuthPreservesRequestContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestContextKey{}, "kept"))
	rec := httptest.NewRecorder()

	auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Context().Value(requestContextKey{}); got != "kept" {
			t.Fatalf("context value = %#v, want kept", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestAuthAllowsRuntimeEventsQueryToken(t *testing.T) {
	if err := db.InitDatabase(t.TempDir()); err != nil {
		t.Fatalf("init database: %v", err)
	}

	token, err := orchestrator.CreateUser(context.Background(), "admin", "abc123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	handler := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value("user") == nil {
			http.Error(w, "missing user", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("runtime events query token authenticates", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, runtimeEventsAPIPath+"?access_token="+token, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status code = %d, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("other endpoints still require bearer header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/runtime/overview?access_token="+token, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}
