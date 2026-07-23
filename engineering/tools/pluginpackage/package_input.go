package main

import (
	"errors"
	"fmt"
	"os"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

func loadExistingPackage(filename string) ([]byte, string, string, string, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, "", "", "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > artifactapi.DefaultMaxArtifactBytes {
		return nil, "", "", "", fmt.Errorf("候选制品必须是 1-%d 字节的普通文件", artifactapi.DefaultMaxArtifactBytes)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, "", "", "", err
	}
	manifest, _, err := artifacttrust.InspectPackage(raw)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("候选包内容无效: %w", err)
	}
	if manifest.ID == "" || manifest.Version == "" || manifest.Publisher == "" {
		return nil, "", "", "", errors.New("候选包清单身份不完整")
	}
	return raw, manifest.ID, manifest.Version, manifest.Publisher, nil
}
