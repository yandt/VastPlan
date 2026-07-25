package sharedstatebackup

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type BackupOptions struct {
	Bucket      string
	Directory   string
	KeyID       string
	PrivateKey  ed25519.PrivateKey
	MaxAttempts int
	Validators  []ValidatorFactory
}

func Backup(ctx context.Context, nc *nats.Conn, js jetstream.JetStream, kv jetstream.KeyValue, options BackupOptions) (Archive, error) {
	if nc == nil || js == nil || kv == nil || !safeToken(options.Bucket) || !filepath.IsAbs(options.Directory) {
		return Archive{}, errors.New("Shared State 备份输入无效")
	}
	if !safeName(options.KeyID) || len(options.PrivateKey) != ed25519.PrivateKeySize {
		return Archive{}, errors.New("Shared State 备份必须配置独立 Ed25519 签名密钥")
	}
	if options.MaxAttempts == 0 {
		options.MaxAttempts = 3
	}
	if options.MaxAttempts < 1 || options.MaxAttempts > 10 {
		return Archive{}, errors.New("Shared State 备份重试次数必须为 1-10")
	}
	if _, err := os.Lstat(options.Directory); err == nil {
		return Archive{}, errors.New("Shared State 备份目录已存在，拒绝覆盖")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Archive{}, err
	}
	parent := filepath.Dir(options.Directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return Archive{}, err
	}
	temporary, err := os.MkdirTemp(parent, "."+filepath.Base(options.Directory)+".tmp-")
	if err != nil {
		return Archive{}, err
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o700); err != nil {
		return Archive{}, err
	}

	streamName := "KV_" + options.Bucket
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return Archive{}, fmt.Errorf("打开 Shared State backing stream: %w", err)
	}
	var manifest Manifest
	for attempt := 1; attempt <= options.MaxAttempts; attempt++ {
		before, err := stream.Info(ctx)
		if err != nil {
			return Archive{}, err
		}
		logical, validations, scanErr := Scan(ctx, kv, options.Validators)
		if scanErr != nil {
			after, infoErr := stream.Info(ctx)
			if infoErr == nil && !sameStreamState(before.State, after.State) && attempt < options.MaxAttempts {
				continue
			}
			return Archive{}, scanErr
		}

		snapshotPath := filepath.Join(temporary, SnapshotFilename)
		_ = os.Remove(snapshotPath)
		snapshot, err := os.OpenFile(snapshotPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return Archive{}, err
		}
		digest := sha256.New()
		counter := &countingWriter{target: snapshot}
		configRaw, stateRaw, snapshotErr := nativeSnapshot(ctx, nc, streamName, multiWriter{left: counter, right: digest})
		if snapshotErr == nil {
			snapshotErr = snapshot.Sync()
		}
		closeErr := snapshot.Close()
		if snapshotErr != nil {
			return Archive{}, snapshotErr
		}
		if closeErr != nil {
			return Archive{}, closeErr
		}
		var snapshotState jetstream.StreamState
		var snapshotConfig jetstream.StreamConfig
		if err := json.Unmarshal(stateRaw, &snapshotState); err != nil {
			return Archive{}, fmt.Errorf("解析 snapshot stream state: %w", err)
		}
		if err := json.Unmarshal(configRaw, &snapshotConfig); err != nil || snapshotConfig.Name != streamName {
			return Archive{}, errors.New("snapshot stream config 身份无效")
		}
		after, err := stream.Info(ctx)
		if err != nil {
			return Archive{}, err
		}
		if !sameStreamState(before.State, snapshotState) || !sameStreamState(snapshotState, after.State) {
			if attempt < options.MaxAttempts {
				continue
			}
			return Archive{}, errors.New("Shared State 在备份窗口持续写入，无法取得一致快照")
		}
		if logical.MaxRevision > snapshotState.LastSeq {
			return Archive{}, errors.New("Shared State 逻辑 revision 超过 snapshot 边界")
		}
		manifest = Manifest{
			Format: ManifestFormat, CreatedAt: time.Now().UTC(), Bucket: options.Bucket, Stream: streamName,
			Snapshot: SnapshotDescriptor{SHA256: hex.EncodeToString(digest.Sum(nil)), Bytes: counter.count},
			Logical:  logical, StreamConfig: configRaw, StreamState: stateRaw, Validations: validations,
		}
		break
	}
	if manifest.Format == "" {
		return Archive{}, errors.New("Shared State 备份未生成一致快照")
	}
	manifestRaw, err := MarshalManifest(manifest)
	if err != nil {
		return Archive{}, err
	}
	signature, err := SignManifest(manifestRaw, options.KeyID, options.PrivateKey)
	if err != nil {
		return Archive{}, err
	}
	signatureRaw, err := json.MarshalIndent(signature, "", "  ")
	if err != nil {
		return Archive{}, err
	}
	if err := writePrivateFile(filepath.Join(temporary, ManifestFilename), manifestRaw); err != nil {
		return Archive{}, err
	}
	if err := writePrivateFile(filepath.Join(temporary, SignatureFilename), append(signatureRaw, '\n')); err != nil {
		return Archive{}, err
	}
	if err := syncDirectory(temporary); err != nil {
		return Archive{}, err
	}
	if err := os.Rename(temporary, options.Directory); err != nil {
		return Archive{}, err
	}
	if err := syncDirectory(parent); err != nil {
		return Archive{}, err
	}
	return Archive{Directory: options.Directory, Manifest: manifest, ManifestRaw: manifestRaw, ManifestHash: ManifestSHA256(manifestRaw)}, nil
}

func sameStreamState(left, right jetstream.StreamState) bool {
	return left.Msgs == right.Msgs && left.Bytes == right.Bytes && left.FirstSeq == right.FirstSeq && left.LastSeq == right.LastSeq
}

type countingWriter struct {
	target interface{ Write([]byte) (int, error) }
	count  int64
}

func (writer *countingWriter) Write(value []byte) (int, error) {
	written, err := writer.target.Write(value)
	writer.count += int64(written)
	return written, err
}

type multiWriter struct {
	left  interface{ Write([]byte) (int, error) }
	right interface{ Write([]byte) (int, error) }
}

func (writer multiWriter) Write(value []byte) (int, error) {
	written, err := writer.left.Write(value)
	if err != nil {
		return written, err
	}
	if written != len(value) {
		return written, errors.New("Shared State snapshot short write")
	}
	if _, err := writer.right.Write(value); err != nil {
		return 0, err
	}
	return written, nil
}
