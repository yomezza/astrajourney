package integration

import (
	"bytes"
	"image/png"
	"io/fs"
	"testing"
)

// Pixel-art v1 replaced the old 512px ground pack. Verify the six external
// opaque 32px ground tiles used by features/planet/art-pack.js.
func TestSurfaceTextureAssets125(t *testing.T) {
	for name, path := range map[string]string{"grass": "grass/grass", "dry": "dirt/dry_soil", "sand": "sand/sand", "rock": "rock/rocky_ground", "snow": "snow/snow", "volcanic": "volcanic/volcanic"} {
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(graphicAssets, "planet/pixelart-v1/terrain/"+path+".png")
			if err != nil {
				t.Fatal(err)
			}
			im, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if im.Bounds().Dx() != 32 || im.Bounds().Dy() != 32 {
				t.Fatal("invalid material size")
			}
			for i := 0; i < 32; i++ {
				for j := 0; j < 32; j++ {
					_, _, _, a := im.At(i, j).RGBA()
					if a != 65535 {
						t.Fatal("unexpected transparent ground")
					}
				}
			}
		})
	}
}
