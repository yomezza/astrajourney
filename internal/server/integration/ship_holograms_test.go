package integration

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yomezza/cosmosimulator/internal/fleet"
)

func TestShipHolograms(t *testing.T) {
	silhouettes := map[string]string{}
	for _, class := range fleet.Catalogue {
		t.Run(class.ID, func(t *testing.T) {
			data, err := fs.ReadFile(graphicAssets, "space/ships/"+class.Model)
			if err != nil {
				t.Fatal(err)
			}
			decoder := xml.NewDecoder(bytes.NewReader(data))
			fills := []string{"black"}
			ids := map[string]bool{}
			refs := []string{}
			orbits, pulses, uses := 0, 0, 0
			engines := 0
			shape := ""
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				switch el := token.(type) {
				case xml.StartElement:
					attrs := map[string]string{}
					for _, a := range el.Attr {
						attrs[a.Name.Local] = a.Value
						if strings.HasPrefix(strings.ToLower(a.Name.Local), "on") {
							t.Fatal("script handler in hologram")
						}
					}
					fill := fills[len(fills)-1]
					if v, ok := attrs["fill"]; ok {
						fill = v
					}
					fills = append(fills, fill)
					if id := attrs["id"]; id != "" {
						if ids[id] {
							t.Fatal("duplicate SVG id", id)
						}
						ids[id] = true
					}
					switch el.Name.Local {
					case "g":
						if attrs["class"] == "engine-thrust" {
							engines++
							if attrs["visibility"] != "hidden" {
								t.Fatal("stationary hull has active engines")
							}
						}
					case "script", "foreignObject", "image", "text":
						t.Fatal("hologram must contain only vector geometry", el.Name.Local)
					case "path", "line", "polyline", "polygon", "rect", "circle", "ellipse":
						if fill != "none" {
							t.Fatal("filled shape/background in hologram", el.Name.Local)
						}
						if shape == "" && el.Name.Local == "path" {
							shape = attrs["d"]
						}
					case "use":
						uses++
						ref := attrs["href"]
						if !strings.HasPrefix(ref, "#") {
							t.Fatal("external hologram dependency", ref)
						}
						refs = append(refs, strings.TrimPrefix(ref, "#"))
					case "animate":
						if attrs["repeatCount"] != "indefinite" {
							t.Fatal("animation must loop")
						}
						switch attrs["attributeName"] {
						case "d":
							orbits++
							frames := strings.Split(attrs["values"], ";")
							if len(frames) < 33 || frames[0] != frames[len(frames)-1] {
								t.Fatal("spatial turn does not close smoothly")
							}
							if attrs["dur"] != "16s" {
								t.Fatal("orbit period must be 16 seconds")
							}
						case "stroke-dashoffset":
							pulses++
						}
					}
				case xml.EndElement:
					fills = fills[:len(fills)-1]
				}
			}
			if orbits < 1 || pulses < 1 || uses < 1 || shape == "" || engines != 1 {
				t.Fatal("missing rotation, light pulses or wireframe")
			}
			for _, ref := range refs {
				if !ids[ref] {
					t.Fatal("missing referenced geometry", ref)
				}
			}
			if other := silhouettes[shape]; other != "" {
				t.Fatal("same hull geometry as", other)
			}
			silhouettes[shape] = class.ID
		})
	}
}

// Every orbit frame must fit its viewport, including the long active engine
// particles. Idle hulls must use most of the preview instead of empty SVG space.
func TestShipHologramViewports(t *testing.T) {
	point := regexp.MustCompile(`[ML](-?[0-9.]+) (-?[0-9.]+)`)
	for _, class := range fleet.Catalogue {
		t.Run(class.ID, func(t *testing.T) {
			data, err := fs.ReadFile(graphicAssets, "space/ships/"+class.Model)
			if err != nil {
				t.Fatal(err)
			}
			var svg struct {
				Idle   string `xml:"data-idle-viewbox,attr"`
				Moving string `xml:"data-moving-viewbox,attr"`
				Paths  []struct {
					ID        string `xml:"id,attr"`
					Animation struct {
						Values string `xml:"values,attr"`
					} `xml:"animate"`
				} `xml:"defs>path"`
			}
			if err = xml.Unmarshal(data, &svg); err != nil {
				t.Fatal(err)
			}
			for _, moving := range []bool{false, true} {
				viewport := svg.Idle
				if moving {
					viewport = svg.Moving
				}
				var x, y, w, h float64
				if n, e := fmt.Sscanf(viewport, "%f %f %f %f", &x, &y, &w, &h); e != nil || n != 4 || w <= 0 || h <= 0 {
					t.Fatal("invalid viewport", viewport)
				}
				minX, maxX, minY, maxY := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
				for _, path := range svg.Paths {
					if !moving && (strings.HasSuffix(path.ID, "-thrust") || strings.HasSuffix(path.ID, "-particles")) {
						continue
					}
					points := point.FindAllStringSubmatch(path.Animation.Values, -1)
					if len(points) == 0 {
						t.Fatal("missing animated geometry", path.ID)
					}
					for _, p := range points {
						px, _ := strconv.ParseFloat(p[1], 64)
						py, _ := strconv.ParseFloat(p[2], 64)
						if px < x+5 || px > x+w-5 || py < y+5 || py > y+h-5 {
							t.Fatalf("clipped geometry %s at %v (moving=%v)", path.ID, p, moving)
						}
						minX = math.Min(minX, px)
						maxX = math.Max(maxX, px)
						minY = math.Min(minY, py)
						maxY = math.Max(maxY, py)
					}
				}
				if !moving && math.Max((maxX-minX)/w, (maxY-minY)/h) < .65 {
					t.Fatal("idle hull remains too small")
				}
			}
		})
	}
}
