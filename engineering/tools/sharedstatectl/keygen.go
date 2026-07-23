package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstatebackup"
)

func runKeygen(arguments []string) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	keyID := flags.String("key-id", "", "稳定签名 key ID")
	privateOut := flags.String("private-out", "", "PKCS#8 Ed25519 私钥输出路径")
	trustOut := flags.String("trust-out", "", "公开信任文档输出路径")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *keyID == "" || *privateOut == "" || *trustOut == "" || !filepath.IsAbs(*privateOut) || !filepath.IsAbs(*trustOut) {
		return fmt.Errorf("keygen 必须指定 key-id 和两个绝对输出路径")
	}
	private, trust, err := sharedstatebackup.GenerateSigningKey(*keyID)
	if err != nil {
		return err
	}
	privateRaw, err := sharedstatebackup.MarshalPrivateKeyPEM(private)
	if err != nil {
		return err
	}
	trustRaw, err := sharedstatebackup.MarshalTrustDocument(trust)
	if err != nil {
		return err
	}
	if err := writeExclusive(*privateOut, privateRaw, 0o600); err != nil {
		return err
	}
	if err := writeExclusive(*trustOut, trustRaw, 0o644); err != nil {
		_ = os.Remove(*privateOut)
		return err
	}
	fmt.Printf("已生成 Shared State 备份签名密钥 keyId=%s\n私钥: %s\n信任文档: %s\n", *keyID, *privateOut, *trustOut)
	return nil
}

func writeExclusive(filename string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
