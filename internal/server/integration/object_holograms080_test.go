package integration

import (
	"bytes"
	"encoding/xml"
	"io"
	"io/fs"
	"strings"
	"testing"
)

func TestObjectHolograms080(t *testing.T) {
	files, err := fs.Glob(testAssets, "web/object-*.svg")
	// The research catalogue also contains a static unknown-object placeholder.
	// Its drawing is not one of the seventeen animated object holograms.
	holograms := files[:0]
	for _, file := range files {
		if file != "web/object-unknown.svg" {
			holograms = append(holograms, file)
		}
	}
	files = holograms
	if err != nil || len(files) != 17 {
		t.Fatal("expected seventeen object holograms", len(files), err)
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			data, err := testAssets.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			decoder := xml.NewDecoder(bytes.NewReader(data))
			rotation, pulse := 0, 0
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				el, ok := token.(xml.StartElement)
				if !ok {
					continue
				}
				attrs := map[string]string{}
				for _, a := range el.Attr {
					attrs[a.Name.Local] = a.Value
					if strings.HasPrefix(strings.ToLower(a.Name.Local), "on") {
						t.Fatal("unexpected script handler")
					}
				}
				switch el.Name.Local {
				case "script", "foreignObject", "image", "text", "rect":
					t.Fatal("unexpected filled or external shape", el.Name.Local)
				case "svg":
					if attrs["fill"] != "none" {
						t.Fatal("object background is not transparent")
					}
				case "animate":
					if attrs["attributeName"] == "d" {
						rotation++
						frames := strings.Split(attrs["values"], ";")
						if attrs["dur"] != "16s" || len(frames) != 33 || frames[0] != frames[len(frames)-1] {
							t.Fatal("incorrect spatial rotation")
						}
					} else if attrs["attributeName"] == "stroke-dashoffset" {
						pulse++
					}
				case "use":
					if !strings.HasPrefix(attrs["href"], "#") {
						t.Fatal("external dependency")
					}
				}
			}
			if rotation != 1 || pulse != 1 {
				t.Fatal("missing hologram animation")
			}
		})
	}
}
