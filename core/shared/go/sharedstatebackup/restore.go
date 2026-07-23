package sharedstatebackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type RestoreOptions struct {
	ConfirmManifestSHA256 string
	WritersStopped        bool
	Validators            []ValidatorFactory
}

type RestoreResult struct {
	Bucket       string
	Stream       string
	Logical      LogicalSummary
	Validations  []ValidationResult
	ManifestHash string
}

func Restore(ctx context.Context, nc *nats.Conn, js jetstream.JetStream, archive Archive, options RestoreOptions) (RestoreResult, error) {
	if nc == nil || js == nil || archive.Manifest.Format != ManifestFormat || archive.ManifestHash == "" {
		return RestoreResult{}, errors.New("Shared State 恢复输入无效")
	}
	if !options.WritersStopped {
		return RestoreResult{}, errors.New("Shared State 恢复前必须确认所有 writer 已停写并撤销旧集群入口")
	}
	if options.ConfirmManifestSHA256 != archive.ManifestHash {
		return RestoreResult{}, errors.New("Shared State 恢复确认摘要与已验签 manifest 不一致")
	}
	if _, err := js.Stream(ctx, archive.Manifest.Stream); err == nil {
		return RestoreResult{}, errors.New("Shared State 目标 stream 已存在，拒绝覆盖或原地恢复")
	} else if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return RestoreResult{}, fmt.Errorf("检查 Shared State 目标 stream: %w", err)
	}
	snapshotPath := filepath.Join(archive.Directory, SnapshotFilename)
	snapshot, err := os.Open(snapshotPath)
	if err != nil {
		return RestoreResult{}, err
	}
	defer snapshot.Close()
	digest := sha256.New()
	counter := &countingReader{source: io.TeeReader(snapshot, digest)}
	if err := nativeRestore(ctx, nc, archive.Manifest.Stream, archive.Manifest.StreamConfig, archive.Manifest.StreamState, counter); err != nil {
		return RestoreResult{}, err
	}
	if counter.count != archive.Manifest.Snapshot.Bytes || hex.EncodeToString(digest.Sum(nil)) != archive.Manifest.Snapshot.SHA256 {
		return RestoreResult{}, errors.New("Shared State snapshot 在恢复读取期间发生变化")
	}
	stream, err := js.Stream(ctx, archive.Manifest.Stream)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("打开恢复后的 Shared State stream: %w", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return RestoreResult{}, err
	}
	var expectedState jetstream.StreamState
	if err := json.Unmarshal(archive.Manifest.StreamState, &expectedState); err != nil {
		return RestoreResult{}, err
	}
	if !sameStreamState(expectedState, info.State) {
		return RestoreResult{}, errors.New("Shared State 恢复后的 stream state 与备份不一致")
	}
	kv, err := js.KeyValue(ctx, archive.Manifest.Bucket)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("打开恢复后的 Shared State KV: %w", err)
	}
	logical, validations, err := Scan(ctx, kv, options.Validators)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("验证恢复后的 Shared State: %w", err)
	}
	if logical != archive.Manifest.Logical || !reflect.DeepEqual(validations, archive.Manifest.Validations) {
		return RestoreResult{}, errors.New("Shared State 恢复后的逻辑摘要或领域验证结果与备份不一致")
	}
	return RestoreResult{
		Bucket: archive.Manifest.Bucket, Stream: archive.Manifest.Stream, Logical: logical,
		Validations: validations, ManifestHash: archive.ManifestHash,
	}, nil
}

type countingReader struct {
	source io.Reader
	count  int64
}

func (reader *countingReader) Read(value []byte) (int, error) {
	read, err := reader.source.Read(value)
	reader.count += int64(read)
	return read, err
}
