// Package credentialsstate owns the persisted Credentials snapshot root
// format. Runtime code and trusted recovery validators share this package so
// the security-critical wire shape has one definition.
package credentialsstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	PluginID        = "cn.vastplan.platform.security.credentials"
	Namespace       = "credentials.ledger"
	RootKey         = "root"
	BlobPrefix      = "blob."
	SnapshotFormat  = "credentials.snapshot.v1"
	ChunkBytes      = 512 << 10
	MaxSnapshotSize = 64 << 20
)

type Chunk struct {
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type Root struct {
	Format string  `json:"format"`
	Digest string  `json:"digest"`
	Size   int     `json:"size"`
	Chunks []Chunk `json:"chunks"`
}

func NewRoot(snapshot []byte) (Root, error) {
	if len(snapshot) == 0 || len(snapshot) > MaxSnapshotSize {
		return Root{}, fmt.Errorf("Credentials tenant 快照必须为 1-%d 字节", MaxSnapshotSize)
	}
	return Root{Format: SnapshotFormat, Digest: DigestHex(snapshot), Size: len(snapshot)}, nil
}

func ParseRoot(raw []byte) (Root, error) {
	var root Root
	if err := DecodeStrictJSON(raw, &root); err != nil {
		return root, fmt.Errorf("解析 Credentials Shared State Root: %w", err)
	}
	if root.Format != SnapshotFormat || len(root.Digest) != sha256.Size*2 || root.Size < 1 || root.Size > MaxSnapshotSize || len(root.Chunks) == 0 || len(root.Chunks) > (MaxSnapshotSize+ChunkBytes-1)/ChunkBytes {
		return root, errors.New("Credentials Shared State Root 无效")
	}
	total := 0
	for _, chunk := range root.Chunks {
		if len(chunk.Digest) != sha256.Size*2 || chunk.Size < 1 || chunk.Size > ChunkBytes {
			return root, errors.New("Credentials Shared State Root chunk 无效")
		}
		if !validDigest(chunk.Digest) {
			return root, errors.New("Credentials Shared State Root chunk 摘要无效")
		}
		total += chunk.Size
	}
	if total != root.Size {
		return root, errors.New("Credentials Shared State Root 总大小无效")
	}
	if !validDigest(root.Digest) {
		return root, errors.New("Credentials Shared State Root 摘要无效")
	}
	return root, nil
}

func DigestHex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func DecodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON 包含尾随数据")
	}
	return nil
}

func validDigest(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}
