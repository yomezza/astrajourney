package main

import (
	"flag"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/webbundle"
	"log"
)

func main() {
	root := flag.String("root", "web", "browser source directory")
	output := flag.String("out", "dist/web", "archive output directory")
	tag := flag.String("tag", "v0.30.1", "published release tag")
	flag.Parse()
	release, e := webbundle.Build(*root, *output, *tag)
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("Web package: %s (%d bytes)\nRevision: %s\n", release.Archive, release.Size, release.Revision)
}
