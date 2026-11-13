// astra-guide-assets generates/checks the trusted SVG manifest, never game data.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/yomezza/cosmosimulator/internal/knowledge"
)

func main() {
	check := flag.Bool("check", false, "verify the checked-in trusted asset manifest")
	flag.Parse()
	raw, err := knowledge.CompileAssets(os.DirFS("internal/server/web"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	path := "internal/knowledge/builtin-assets.json"
	if *check {
		old, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(old, raw) {
			fmt.Fprintln(os.Stderr, "trusted SVG manifest is stale; run go run ./cmd/astra-guide-assets")
			os.Exit(1)
		}
		fmt.Println("Trusted SVG manifest verified")
		return
	}
	if err = os.WriteFile(path, raw, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
