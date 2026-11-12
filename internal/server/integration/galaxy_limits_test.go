package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yomezza/cosmosimulator/internal/universe"
)

func TestCreateGalaxySizeBoundaries(t *testing.T) {
	f := newFixture033(t)
	cookie, uid := f.signup("galaxy_bounds")
	f.exec(`UPDATE users SET role='admin' WHERE id=$1`, uid)
	web := httptest.NewUnstartedServer(f.handler)
	web.Config.WriteTimeout = 15 * time.Second // Same transport limit as the real Cosmic process.
	web.Start()
	defer web.Close()
	client := &http.Client{Timeout: 2 * time.Minute}
	call := func(path string, in universe.GalaxyEdit, status int) []byte {
		raw, _ := json.Marshal(in)
		request, err := http.NewRequest(http.MethodPost, web.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("%s: %d != %d %s", path, response.StatusCode, status, body)
		}
		return body
	}
	for _, count := range []int{4, 50001} {
		call("/api/admin/galaxies/preview", universe.GalaxyEdit{Create: true, ID: "invalid-bounds", Name: "Invalid", Shape: "rings", Systems: count, Settings: universe.DefaultGalaxySettings()}, 400)
	}
	for _, count := range []int{5, 50000} {
		start := time.Now()
		in := universe.GalaxyEdit{Create: true, ID: fmt.Sprintf("bounds-%d", count), Name: fmt.Sprintf("Bounds %d", count), Shape: "rings", Seed: 1794461000423, Systems: count, Settings: universe.DefaultGalaxySettings()}
		response := call("/api/admin/galaxies/preview", in, 200)
		var preview struct {
			Preview string `json:"preview"`
		}
		if err := json.Unmarshal(response, &preview); err != nil {
			t.Fatal(err)
		}
		t.Logf("preview %d systems: %s", count, time.Since(start))
		in.Preview = preview.Preview
		call("/api/admin/galaxies/apply", in, 200)
		g, err := f.app.World.Load(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(universe.GalaxyStars(g, in.ID)) != count {
			t.Fatal("galaxy size mismatch")
		}
		t.Logf("preview + apply %d systems: %s", count, time.Since(start))
	}
}
