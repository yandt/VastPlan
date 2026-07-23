// sbom 为 Go 二进制生成确定性的 CycloneDX 1.5 SBOM。
package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/engineering/internal/cyclonedx"
)

func main() {
	binary := flag.String("binary", "", "Go binary")
	output := flag.String("output", "", "SBOM output")
	name := flag.String("name", "backend-kernel", "component name")
	version := flag.String("version", "", "component version")
	flag.Parse()
	if *binary == "" || *output == "" || *version == "" {
		flag.Usage()
		os.Exit(2)
	}
	file, err := os.Open(*binary)
	if err != nil {
		fatal(err)
	}
	digester := sha256.New()
	if _, err := io.Copy(digester, file); err != nil {
		_ = file.Close()
		fatal(err)
	}
	if err := file.Close(); err != nil {
		fatal(err)
	}
	var digest [sha256.Size]byte
	copy(digest[:], digester.Sum(nil))
	info, err := buildinfo.ReadFile(*binary)
	if err != nil {
		fatal(err)
	}
	rootRef := "pkg:generic/" + *name + "@" + *version
	root := cyclonedx.Component{
		Type: "application", BOMRef: rootRef, Name: *name, Version: *version, PURL: rootRef,
		Hashes: []cyclonedx.Hash{{Alg: "SHA-256", Content: hex.EncodeToString(digest[:])}},
	}
	components := make([]cyclonedx.Component, 0, len(info.Deps))
	for _, dep := range info.Deps {
		module := dep
		if dep.Replace != nil {
			module = dep.Replace
		}
		purl := "pkg:golang/" + module.Path + "@" + module.Version
		components = append(components, cyclonedx.Component{
			Type: "library", BOMRef: purl, Name: module.Path, Version: module.Version, PURL: purl,
		})
	}
	doc, err := cyclonedx.Build(root, components, digest[:])
	if err != nil {
		fatal(err)
	}
	encoded, err := cyclonedx.Marshal(doc)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
