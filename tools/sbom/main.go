// sbom 为 Go 二进制生成确定性的 CycloneDX 1.5 SBOM。
package main

import (
	"crypto/sha1" // UUIDv5 规范算法，仅生成标识，不用于安全校验。
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type bom struct {
	BOMFormat    string      `json:"bomFormat"`
	SpecVersion  string      `json:"specVersion"`
	Version      int         `json:"version"`
	SerialNumber string      `json:"serialNumber"`
	Metadata     metadata    `json:"metadata"`
	Components   []component `json:"components"`
}

type metadata struct {
	Component component `json:"component"`
}

type component struct {
	Type    string `json:"type"`
	BOMRef  string `json:"bom-ref,omitempty"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	PURL    string `json:"purl,omitempty"`
	Hashes  []hash `json:"hashes,omitempty"`
}

type hash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

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
	root := component{
		Type: "application", BOMRef: rootRef, Name: *name, Version: *version, PURL: rootRef,
		Hashes: []hash{{Alg: "SHA-256", Content: hex.EncodeToString(digest[:])}},
	}
	components := make([]component, 0, len(info.Deps))
	for _, dep := range info.Deps {
		module := dep
		if dep.Replace != nil {
			module = dep.Replace
		}
		purl := "pkg:golang/" + module.Path + "@" + module.Version
		components = append(components, component{
			Type: "library", BOMRef: purl, Name: module.Path, Version: module.Version, PURL: purl,
		})
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Name == components[j].Name {
			return components[i].Version < components[j].Version
		}
		return components[i].Name < components[j].Name
	})
	uuid := deterministicUUID(digest)
	doc := bom{
		BOMFormat: "CycloneDX", SpecVersion: "1.5", Version: 1,
		SerialNumber: "urn:uuid:" + uuid,
		Metadata:     metadata{Component: root}, Components: components,
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		fatal(err)
	}
}

func deterministicUUID(digest [sha256.Size]byte) string {
	// RFC 4122 URL namespace。把二进制 SHA-256 当作名称输入，得到合法且确定的 UUIDv5。
	namespace := [...]byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	hasher := sha1.New()
	_, _ = hasher.Write(namespace[:])
	_, _ = hasher.Write(digest[:])
	value := hasher.Sum(nil)[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80 // RFC 4122 variant。
	hexValue := hex.EncodeToString(value)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexValue[:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
