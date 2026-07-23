package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstatebackup"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsbackup"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
)

func TestSignedNativeBackupRestorePreservesCASAndCredentialsIntegrity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nc, js := startNATS(t)
	metadata, _ := sharedstate.DevelopmentCapacityPolicy().Metadata()
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: controlplane.SharedStateBucket, History: 64, Storage: jetstream.FileStorage,
		MaxValueSize: sharedstate.MaxValueBytes, MaxBytes: sharedstate.DevelopmentMaxBytes, Metadata: metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := sharedstate.NewNATSStore(kv)
	credentialScope := sharedstate.Scope{
		Kind: sharedstate.ScopeTenant, TenantID: "tenant-a", PluginID: credentialsstate.PluginID,
		RuntimeScope: "platform-credentials", Namespace: credentialsstate.Namespace,
	}
	snapshot := []byte(`{"records":{"database":{"ciphertext":"vault:v1:test"}}}`)
	first, second := snapshot[:20], snapshot[20:]
	root, err := credentialsstate.NewRoot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range [][]byte{first, second} {
		digest := credentialsstate.DigestHex(chunk)
		root.Chunks = append(root.Chunks, credentialsstate.Chunk{Digest: digest, Size: len(chunk)})
		if _, err := store.Create(ctx, credentialScope, credentialsstate.BlobPrefix+digest, chunk); err != nil {
			t.Fatal(err)
		}
	}
	rootRaw, _ := json.Marshal(root)
	rootEntry, err := store.Create(ctx, credentialScope, credentialsstate.RootKey, rootRaw)
	if err != nil {
		t.Fatal(err)
	}
	settingsScope := sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: "tenant-a", PluginID: "cn.vastplan.settings", RuntimeScope: "platform-settings", Namespace: "values"}
	setting, err := store.Create(ctx, settingsScope, "theme", []byte(`{"mode":"light"}`))
	if err != nil {
		t.Fatal(err)
	}
	setting, err = store.Update(ctx, settingsScope, "theme", []byte(`{"mode":"dark"}`), setting.Revision)
	if err != nil {
		t.Fatal(err)
	}

	private, trust, err := sharedstatebackup.GenerateSigningKey("backup-2026-01")
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "shared-state-backup")
	archive, err := sharedstatebackup.Backup(ctx, nc, js, kv, sharedstatebackup.BackupOptions{
		Bucket: controlplane.SharedStateBucket, Directory: directory, KeyID: "backup-2026-01", PrivateKey: private,
		Validators: []sharedstatebackup.ValidatorFactory{credentialsbackup.Factory{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := sharedstatebackup.VerifyArchive(directory, trust)
	if err != nil || verified.Manifest.Logical.Entries != 4 || verified.Manifest.Validations[0].Counters["roots"] != 1 {
		t.Fatalf("verify archive: manifest=%+v err=%v", verified.Manifest, err)
	}
	if _, err := sharedstatebackup.Restore(ctx, nc, js, verified, sharedstatebackup.RestoreOptions{
		ConfirmManifestSHA256: verified.ManifestHash, WritersStopped: true,
		Validators: []sharedstatebackup.ValidatorFactory{credentialsbackup.Factory{}},
	}); err == nil {
		t.Fatal("目标 stream 存在时不得原地恢复")
	}
	if err := js.DeleteKeyValue(ctx, controlplane.SharedStateBucket); err != nil {
		t.Fatal(err)
	}
	result, err := sharedstatebackup.Restore(ctx, nc, js, verified, sharedstatebackup.RestoreOptions{
		ConfirmManifestSHA256: verified.ManifestHash, WritersStopped: true,
		Validators: []sharedstatebackup.ValidatorFactory{credentialsbackup.Factory{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Logical != archive.Manifest.Logical || result.Validations[0].Counters["orphanChunks"] != 0 {
		t.Fatalf("restore result=%+v", result)
	}
	restoredKV, err := js.KeyValue(ctx, controlplane.SharedStateBucket)
	if err != nil {
		t.Fatal(err)
	}
	restoredStore, _ := sharedstate.NewNATSStore(restoredKV)
	restoredRoot, err := restoredStore.Get(ctx, credentialScope, credentialsstate.RootKey)
	if err != nil || restoredRoot.Revision != rootEntry.Revision {
		t.Fatalf("Credentials Root revision 未保留: got=%+v want=%d err=%v", restoredRoot, rootEntry.Revision, err)
	}
	restoredSetting, err := restoredStore.Get(ctx, settingsScope, "theme")
	if err != nil || restoredSetting.Revision != setting.Revision || string(restoredSetting.Value) != `{"mode":"dark"}` {
		t.Fatalf("setting 未恢复: got=%+v wantRevision=%d err=%v", restoredSetting, setting.Revision, err)
	}
	if _, err := restoredStore.Update(ctx, settingsScope, "theme", []byte(`{"mode":"system"}`), restoredSetting.Revision); err != nil {
		t.Fatalf("恢复后的 CAS revision 不可继续使用: %v", err)
	}
	var capacityOutput bytes.Buffer
	if err := runCapacity([]string{"-nats-url", nc.ConnectedUrl(), "-nats-allow-insecure", "-fail-on", "none"}, &capacityOutput); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(capacityOutput.Bytes(), []byte(`"maxBytes":1073741824`)) || bytes.Contains(capacityOutput.Bytes(), []byte("tenant-a")) {
		t.Fatalf("capacity 输出不正确或泄漏租户: %s", capacityOutput.String())
	}
}

func TestArchiveTamperingAndRestoreConfirmationFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	nc, js := startNATS(t)
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: controlplane.SharedStateBucket, History: 64, Storage: jetstream.FileStorage})
	if err != nil {
		t.Fatal(err)
	}
	private, trust, _ := sharedstatebackup.GenerateSigningKey("backup-test")
	directory := filepath.Join(t.TempDir(), "backup")
	archive, err := sharedstatebackup.Backup(ctx, nc, js, kv, sharedstatebackup.BackupOptions{Bucket: controlplane.SharedStateBucket, Directory: directory, KeyID: "backup-test", PrivateKey: private})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sharedstatebackup.Restore(ctx, nc, js, archive, sharedstatebackup.RestoreOptions{ConfirmManifestSHA256: archive.ManifestHash}); err == nil {
		t.Fatal("未确认 writer 停写不得恢复")
	}
	snapshotPath := filepath.Join(directory, sharedstatebackup.SnapshotFilename)
	file, err := os.OpenFile(snapshotPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("tampered"))
	_ = file.Close()
	if _, err := sharedstatebackup.VerifyArchive(directory, trust); err == nil {
		t.Fatal("被修改的 snapshot 不得通过归档校验")
	}
}

func TestBackupRejectsIncompleteCredentialsRoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	nc, js := startNATS(t)
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: controlplane.SharedStateBucket, History: 64, Storage: jetstream.FileStorage})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := sharedstate.NewNATSStore(kv)
	scope := sharedstate.Scope{
		Kind: sharedstate.ScopeTenant, TenantID: "tenant-a", PluginID: credentialsstate.PluginID,
		RuntimeScope: "platform-credentials", Namespace: credentialsstate.Namespace,
	}
	missing := []byte("missing-chunk")
	root := credentialsstate.Root{
		Format: credentialsstate.SnapshotFormat, Digest: credentialsstate.DigestHex(missing), Size: len(missing),
		Chunks: []credentialsstate.Chunk{{Digest: credentialsstate.DigestHex(missing), Size: len(missing)}},
	}
	rootRaw, _ := json.Marshal(root)
	if _, err := store.Create(ctx, scope, credentialsstate.RootKey, rootRaw); err != nil {
		t.Fatal(err)
	}
	private, _, _ := sharedstatebackup.GenerateSigningKey("backup-test")
	directory := filepath.Join(t.TempDir(), "incomplete")
	if _, err := sharedstatebackup.Backup(ctx, nc, js, kv, sharedstatebackup.BackupOptions{
		Bucket: controlplane.SharedStateBucket, Directory: directory, KeyID: "backup-test", PrivateKey: private,
		Validators: []sharedstatebackup.ValidatorFactory{credentialsbackup.Factory{}},
	}); err == nil {
		t.Fatal("缺少 chunk 的 Credentials Root 不得进入备份")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("失败备份不得提交目标目录: %v", err)
	}
}

func startNATS(t *testing.T) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	srv, err := server.NewServer(&server.Options{JetStream: true, StoreDir: filepath.Join(t.TempDir(), "js"), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS 未就绪")
	}
	if _, ok := srv.Addr().(*net.TCPAddr); !ok {
		t.Fatal("NATS 未监听 TCP")
	}
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js
}
