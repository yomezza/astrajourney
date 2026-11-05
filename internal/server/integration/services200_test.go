package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminServices200(t *testing.T) {
	f := newFixture033(t)
	admin, uid := f.signup("services_admin200")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	player, _ := f.signup("services_player200")
	f.req(nil, "GET", "/api/admin/services", nil, 401)
	f.req(player, "GET", "/api/admin/services", nil, 403)
	read := func(want string) {
		t.Helper()
		r := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/admin/services", nil)
		request.AddCookie(admin)
		f.app.Handler().ServeHTTP(r, request)
		if r.Code != 200 {
			t.Fatal("status endpoint failed", r.Code)
		}
		var data struct{ Services []struct{ ID, Status string } }
		if err := json.Unmarshal(r.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if len(data.Services) != 3 || data.Services[2].ID != "chat" || data.Services[0].Status != "ready" || data.Services[1].ID != "planet" || data.Services[1].Status != want {
			t.Fatalf("status report: %s", r.Body.String())
		}
		for _, secret := range []string{"password_hash", "token_hash", "DATABASE_URL"} {
			if strings.Contains(r.Body.String(), secret) {
				t.Fatal("private config exposed")
			}
		}
	}
	f.app.Transfer.Enabled = false
	read("disabled")
	base, stop := startPlanet150(t, f.db)
	t.Setenv("PLANET_INTERNAL_URL", base)
	f.app.Transfer.Enabled = true
	read("ready")
	stop()
	read("unreachable")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("forwarded browser cookie")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"service":"cosmosimulator-planet","status":"ready","gameplay_ready":true,"version":"0.21.0","schema_version":7,"runtime_instance":"00000000-0000-4000-8000-000000000001","runtime_fence":1}`))
	}))
	defer fake.Close()
	t.Setenv("PLANET_INTERNAL_URL", fake.URL)
	read("mismatch")
	t.Setenv("PLANET_INTERNAL_URL", "http://user:password@127.0.0.1:8081")
	read("configuration_error")
}
