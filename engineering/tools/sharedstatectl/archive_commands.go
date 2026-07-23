package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstatebackup"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsbackup"
)

func runBackup(arguments []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	natsOptions := addNATSFlags(flags)
	directory := flags.String("out", "", "新建的绝对备份目录")
	signKey := flags.String("sign-key", "", "独立 Ed25519 PKCS#8 备份签名私钥")
	keyID := flags.String("key-id", "", "签名 key ID")
	attempts := flags.Int("attempts", 3, "检测到并发写入后的完整重试次数")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *directory == "" || !filepath.IsAbs(*directory) || *signKey == "" || *keyID == "" {
		return fmt.Errorf("backup 必须指定绝对 out、sign-key 和 key-id")
	}
	private, err := sharedstatebackup.LoadPrivateKeyPEM(*signKey)
	if err != nil {
		return err
	}
	nc, err := natsOptions.connect("vastplan-shared-state-backup")
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	kv, err := js.KeyValue(context.Background(), controlplane.SharedStateBucket)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	archive, err := sharedstatebackup.Backup(ctx, nc, js, kv, sharedstatebackup.BackupOptions{
		Bucket: controlplane.SharedStateBucket, Directory: *directory, KeyID: *keyID,
		PrivateKey: private, MaxAttempts: *attempts,
		Validators: []sharedstatebackup.ValidatorFactory{credentialsbackup.Factory{}},
	})
	if err != nil {
		return err
	}
	fmt.Printf("Shared State 备份完成 manifestSHA256=%s entries=%d snapshotBytes=%d\n", archive.ManifestHash, archive.Manifest.Logical.Entries, archive.Manifest.Snapshot.Bytes)
	return nil
}

func runVerify(arguments []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	directory := flags.String("archive", "", "绝对备份目录")
	trustFile := flags.String("trust", "", "备份签名信任文档")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *directory == "" || !filepath.IsAbs(*directory) || *trustFile == "" {
		return fmt.Errorf("verify 必须指定绝对 archive 和 trust")
	}
	trust, err := sharedstatebackup.LoadTrustDocument(*trustFile)
	if err != nil {
		return err
	}
	archive, err := sharedstatebackup.VerifyArchive(*directory, trust)
	if err != nil {
		return err
	}
	fmt.Printf("Shared State 备份验签通过 manifestSHA256=%s entries=%d snapshotBytes=%d\n", archive.ManifestHash, archive.Manifest.Logical.Entries, archive.Manifest.Snapshot.Bytes)
	return nil
}

func runRestore(arguments []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	natsOptions := addNATSFlags(flags)
	directory := flags.String("archive", "", "绝对备份目录")
	trustFile := flags.String("trust", "", "备份签名信任文档")
	confirm := flags.String("confirm-manifest-sha256", "", "verify 输出的完整 manifest SHA-256")
	writersStopped := flags.Bool("confirm-writers-stopped", false, "确认全部 writer 已停写且旧集群入口已撤销")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *directory == "" || !filepath.IsAbs(*directory) || *trustFile == "" || *confirm == "" {
		return fmt.Errorf("restore 必须指定绝对 archive、trust 和 confirm-manifest-sha256")
	}
	trust, err := sharedstatebackup.LoadTrustDocument(*trustFile)
	if err != nil {
		return err
	}
	archive, err := sharedstatebackup.VerifyArchive(*directory, trust)
	if err != nil {
		return err
	}
	nc, err := natsOptions.connect("vastplan-shared-state-restore")
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	result, err := sharedstatebackup.Restore(ctx, nc, js, archive, sharedstatebackup.RestoreOptions{
		ConfirmManifestSHA256: *confirm, WritersStopped: *writersStopped,
		Validators: []sharedstatebackup.ValidatorFactory{credentialsbackup.Factory{}},
	})
	if err != nil {
		return err
	}
	fmt.Printf("Shared State 恢复并复核完成 manifestSHA256=%s entries=%d\n", result.ManifestHash, result.Logical.Entries)
	return nil
}
