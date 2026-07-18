package nodebootstrap

import (
	"fmt"
	"io"
	"os"
)

const maxBootstrapSecretBytes = 4 << 20

// LoadSecretPayloads reads bootstrap material from owner-controlled regular
// files. Public trust documents are intentionally held to the same local
// permission baseline as private material so the request cannot be redirected
// through a writable file.
func LoadSecretPayloads(files []SecretFile) (result []SecretPayload, err error) {
	if len(files) > maxBootstrapSecretFiles {
		return nil, fmt.Errorf("秘密文件不能超过 %d 个", maxBootstrapSecretFiles)
	}
	result = make([]SecretPayload, 0, len(files))
	defer func() {
		if err != nil {
			wipeSecretPayloads(result)
		}
	}()
	total := 0
	for i := range files {
		file := files[i]
		if err := file.Validate(); err != nil {
			return nil, fmt.Errorf("secretFiles[%d]: %w", i, err)
		}
		info, err := os.Lstat(file.Source)
		if err != nil {
			return nil, fmt.Errorf("读取引导文件 %s: %w", file.Source, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("引导文件必须是仅属主可访问的普通文件: %s", file.Source)
		}
		if info.Size() <= 0 || info.Size() > maxBootstrapSecretBytes {
			return nil, fmt.Errorf("引导文件大小无效: %s", file.Source)
		}
		handle, err := os.Open(file.Source)
		if err != nil {
			return nil, fmt.Errorf("打开引导文件 %s: %w", file.Source, err)
		}
		openedInfo, statErr := handle.Stat()
		if statErr != nil || !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() {
			_ = handle.Close()
			return nil, fmt.Errorf("引导文件在读取期间发生替换: %s", file.Source)
		}
		content, readErr := io.ReadAll(io.LimitReader(handle, maxBootstrapSecretBytes+1))
		closeErr := handle.Close()
		if readErr != nil {
			return nil, fmt.Errorf("读取引导文件 %s: %w", file.Source, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("关闭引导文件 %s: %w", file.Source, closeErr)
		}
		if len(content) == 0 || len(content) > maxBootstrapSecretBytes {
			return nil, fmt.Errorf("引导文件大小无效: %s", file.Source)
		}
		total += len(content)
		if total > maxBootstrapSecretTotalBytes {
			for j := range content {
				content[j] = 0
			}
			return nil, fmt.Errorf("引导文件总大小不能超过 %d 字节", maxBootstrapSecretTotalBytes)
		}
		result = append(result, SecretPayload{Destination: file.Destination, Mode: file.Mode, Content: content})
	}
	return result, nil
}

func wipeSecretPayloads(payloads []SecretPayload) {
	for i := range payloads {
		for j := range payloads[i].Content {
			payloads[i].Content[j] = 0
		}
	}
}
