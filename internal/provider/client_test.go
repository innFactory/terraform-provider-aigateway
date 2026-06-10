package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSendsBearerAndAdminHeader(t *testing.T) {
	var gotAuth, gotAdmin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAdmin = r.Header.Get("X-Gateway-Admin-Key")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "provider_1"})
	}))
	defer srv.Close()

	c := newClient(srv.URL, "secret-key", "test")
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(context.Background(), "GET", "/api/v1/admin/providers", nil, nil, &out); err != nil {
		t.Fatalf("do: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want Bearer secret-key", gotAuth)
	}
	if gotAdmin != "secret-key" {
		t.Errorf("X-Gateway-Admin-Key = %q, want secret-key", gotAdmin)
	}
	if out.ID != "provider_1" {
		t.Errorf("decoded id = %q", out.ID)
	}
}

func TestClientMapsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail":     "not found",
			"error_code": "E4040",
		})
	}))
	defer srv.Close()

	c := newClient(srv.URL, "k", "test")
	err := c.do(context.Background(), "GET", "/x", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !isNotFound(err) {
		t.Errorf("isNotFound = false, want true for 404; err=%v", err)
	}
}

func TestClientUnlimitedBudgetSerialisesNull(t *testing.T) {
	// org budget unlimited → orgBudgetLimitMicrodollars must be JSON null,
	// not omitted, so the gateway sets unlimited.
	body := tenantPatchBody{DefaultAllowedModels: []string{"gpt-4o"}, OrgBudgetMicros: nil}
	raw, _ := json.Marshal(body)
	want := `{"defaultAllowedModels":["gpt-4o"],"orgBudgetLimitMicrodollars":null}`
	if string(raw) != want {
		t.Errorf("marshal = %s, want %s", raw, want)
	}
}
