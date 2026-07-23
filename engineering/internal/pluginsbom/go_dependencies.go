package pluginsbom

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"cdsoft.com.cn/VastPlan/engineering/internal/cyclonedx"
)

func goDependencies(filename string) ([]cyclonedx.Component, string, string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, "", "", fmt.Errorf("读取 Go 构建产物: %w", err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return nil, "", "", fmt.Errorf("计算 Go 构建产物摘要: %v", copyErr)
	}
	info, err := buildinfo.ReadFile(filename)
	if err != nil {
		return nil, "", "", fmt.Errorf("读取 Go build info: %w", err)
	}
	components := make([]cyclonedx.Component, 0, len(info.Deps))
	for _, dependency := range info.Deps {
		module := dependency
		if dependency.Replace != nil {
			module = dependency.Replace
		}
		if module.Path == "" || module.Version == "" || module.Version == "(devel)" {
			continue
		}
		purl := "pkg:golang/" + module.Path + "@" + module.Version
		components = append(components, cyclonedx.Component{Type: "library", BOMRef: purl, Name: module.Path, Version: module.Version, PURL: purl, Properties: []cyclonedx.Property{{Name: "vastplan:dependency.evidence", Value: "go-buildinfo"}}})
	}
	return components, hex.EncodeToString(hash.Sum(nil)), info.GoVersion, nil
}
