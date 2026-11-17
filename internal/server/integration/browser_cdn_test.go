package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/cdnserver"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
)

// browserCDN mirrors the public deployment: graphics are external, remaining
// requests go to the real Cosmic handler. No graphics fallback is added to it.
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
	return cdn.Handler()
}
