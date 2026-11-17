package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/cdnserver"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	"github.com/yomezza/cosmosimulator/internal/webbundle"
	"github.com/yomezza/cosmosimulator/internal/webserver"
)

// browserWeb runs the real external UI bundle and gateway in front of CDN/API.
func browserCDN(t *testing.T, f *fixture033, cosmic http.Handler) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(cosmic)
	output := t.TempDir()
	release, _, err := assetpack.Build("../../../assets/game", output, "v0.29.0")
	if err != nil {
		upstream.Close()
		t.Fatal(err)
	}
	root := t.TempDir()
	cdn, err := cdnserver.New(cdnserver.Config{StorageRoot: root, CosmicURL: upstream.URL})
	if err != nil {
		upstream.Close()
		t.Fatal(err)
	}
	if err = cdn.Install(filepath.Join(output, release.Archive)); err != nil {
		cdn.Close()
		upstream.Close()
		t.Fatal(err)
	}
	internal := httptest.NewServer(cdn.Handler())
	f.app.Knowledge.MediaStore = &mediastore.Client{BaseURL: internal.URL, TokenFile: filepath.Join(root, "state", "service.token"), HTTP: internal.Client()}
	t.Cleanup(func() { internal.Close(); upstream.Close(); cdn.Close() })
	planet, chat := os.Getenv("PLANET_INTERNAL_URL"), os.Getenv("CHAT_INTERNAL_URL")
	if planet == "" {
		planet = "http://127.0.0.1:8081"
	}
	if chat == "" {
		chat = "http://127.0.0.1:8082"
	}
	return browserWebGateway(t, upstream.URL, internal.URL, planet, chat)
}
func browserWebGateway(t *testing.T, cosmic, cdn, planet, chat string) http.Handler {
	t.Helper()
	out := t.TempDir()
	release, err := webbundle.Build("../../../web", out, "v0.30.0")
	if err != nil {
		t.Fatal(err)
	}
	service, err := webserver.New(webserver.Config{StorageRoot: t.TempDir(), CosmicURL: cosmic, CDNURL: cdn, PlanetURL: planet, ChatURL: chat})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close() })
	if err = service.Install(filepath.Join(out, release.Archive)); err != nil {
		t.Fatal(err)
	}
	return service.Handler()
}
