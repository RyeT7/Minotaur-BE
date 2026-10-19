package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/RyeT7/Minotaur-BE/internal/inventory"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r, err := NewRouter(inventory.NewStore(nil), Options{AllowedOrigins: []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMovementFlow(t *testing.T) {
	h := newTestRouter(t)

	if rec := do(t, h, http.MethodPost, "/api/v1/items", `{"sku":"WID-1","name":"Widget","warehouse":"WH-A","quantity":5}`); rec.Code != http.StatusCreated {
		t.Fatalf("create item: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, http.MethodPost, "/api/v1/movements", `{"kind":"received","sku":"WID-1","warehouse":"WH-A","quantity":3}`); rec.Code != http.StatusCreated {
		t.Fatalf("record movement: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, http.MethodPost, "/api/v1/movements", `{"kind":"shipped","sku":"WID-1","warehouse":"WH-A","quantity":100}`); rec.Code != http.StatusConflict {
		t.Fatalf("oversell: %d %s", rec.Code, rec.Body)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/items/WID-1/WH-A", "")
	var it inventory.Item
	if err := json.Unmarshal(rec.Body.Bytes(), &it); err != nil || it.Quantity != 8 {
		t.Fatalf("get item: %d %s", rec.Code, rec.Body)
	}

	rec = do(t, h, http.MethodGet, "/api/v1/activity?limit=10", "")
	var act []inventory.Activity
	if err := json.Unmarshal(rec.Body.Bytes(), &act); err != nil || len(act) != 1 || act[0].Kind != inventory.Received {
		t.Fatalf("activity: %d %s", rec.Code, rec.Body)
	}
}

func TestRejectsBadInput(t *testing.T) {
	h := newTestRouter(t)

	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"unknown field", http.MethodPost, "/api/v1/items", `{"sku":"A","name":"B","warehouse":"C","admin":true}`, http.StatusBadRequest},
		{"too large", http.MethodPost, "/api/v1/items", `{"note":"` + strings.Repeat("x", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
		{"bad limit", http.MethodGet, "/api/v1/movements?limit=9999", "", http.StatusBadRequest},
		{"bad days", http.MethodGet, "/api/v1/movements/daily?days=0", "", http.StatusBadRequest},
		{"wrong method", http.MethodDelete, "/api/v1/items", "", http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := do(t, h, tc.method, tc.path, tc.body); rec.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

func TestCORS(t *testing.T) {
	h := newTestRouter(t)

	for origin, want := range map[string]string{
		"http://localhost:5173": "http://localhost:5173",
		"https://evil.example":  "",
	} {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/items", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want || rec.Code != http.StatusNoContent {
			t.Fatalf("origin %s: allow=%q code=%d", origin, got, rec.Code)
		}
	}
}
