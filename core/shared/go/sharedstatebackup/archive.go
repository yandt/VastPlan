package sharedstatebackup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Archive struct {
	Directory    string
	Manifest     Manifest
	ManifestRaw  []byte
	ManifestHash string
}

func VerifyArchive(directory string, trust TrustDocument) (Archive, error) {
	if !filepath.IsAbs(directory) {
		return Archive{}, errors.New("Shared State 备份目录必须是绝对路径")
	}
	if err := requirePrivateDirectory(directory); err != nil {
		return Archive{}, err
	}
	manifestRaw, err := readPrivateFile(filepath.Join(directory, ManifestFilename), MaxManifestBytes)
	if err != nil {
		return Archive{}, err
	}
	manifest, err := ParseManifest(manifestRaw)
	if err != nil {
		return Archive{}, err
	}
	signatureRaw, err := readPrivateFile(filepath.Join(directory, SignatureFilename), MaxSignatureBytes)
	if err != nil {
		return Archive{}, err
	}
	var signature ManifestSignature
	if err := decodeStrictJSON(signatureRaw, &signature); err != nil {
		return Archive{}, fmt.Errorf("解析 Shared State 备份签名: %w", err)
	}
	if err := VerifyManifest(manifestRaw, signature, trust); err != nil {
		return Archive{}, err
	}
	snapshot, err := os.Open(filepath.Join(directory, SnapshotFilename))
	if err != nil {
		return Archive{}, err
	}
	defer snapshot.Close()
	if info, statErr := snapshot.Stat(); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() != manifest.Snapshot.Bytes {
		return Archive{}, errors.New("Shared State snapshot 文件属性与清单不一致")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, snapshot); err != nil {
		return Archive{}, err
	}
	if hex.EncodeToString(digest.Sum(nil)) != manifest.Snapshot.SHA256 {
		return Archive{}, errors.New("Shared State snapshot SHA-256 与清单不一致")
	}
	return Archive{Directory: directory, Manifest: manifest, ManifestRaw: manifestRaw, ManifestHash: ManifestSHA256(manifestRaw)}, nil
}

func requirePrivateDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("Shared State 备份目录必须是 0700 或更严格的真实目录")
	}
	return nil
}

func readPrivateFile(filename string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > maximum {
		return nil, fmt.Errorf("Shared State 备份文件权限或类型无效: %s", filepath.Base(filename))
	}
	return os.ReadFile(filename)
}

func writePrivateFile(filename string, value []byte) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}
