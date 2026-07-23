package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxPackageBytes = int64(256 << 20)

func readRegular(path string, max int64, private bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return nil, fmt.Errorf("文件不存在、不是普通文件或大小超限: %s", path)
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("私钥文件必须仅属主可读写")
	}
	return os.ReadFile(path)
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := readRegular(path, 64<<10, true)
	if err != nil {
		return nil, err
	}
	if block, _ := pem.Decode(raw); block != nil {
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析 PKCS#8 私钥: %w", err)
		}
		key, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 私钥不是 Ed25519")
		}
		return append(ed25519.PrivateKey(nil), key...), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, errors.New("私钥必须是 Ed25519 PKCS#8 PEM 或 base64 raw")
	}
	return ed25519.PrivateKey(decoded), nil
}

func writeAtomic(path string, raw []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".artifact-assessment-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
