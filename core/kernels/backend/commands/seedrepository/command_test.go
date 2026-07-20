package seedrepositorycommand

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProfileRejectsUnknownFieldsAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.json")
	if err := os.WriteFile(profile, []byte(`{"version":1,"id":"seed-repository","listen":"127.0.0.1:8443","repositoryRoot":"relative","trustFile":"/etc/trust","tlsCertFile":"/etc/cert","tlsKeyFile":"/etc/key","readTokenFile":"/etc/read","publishTokenFile":"/etc/publish"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(profile); err == nil {
		t.Fatal("相对 repositoryRoot 必须拒绝")
	}
	if err := os.WriteFile(profile, []byte(`{"version":1,"id":"seed-repository","listen":"127.0.0.1:8443","repositoryRoot":"/var/lib/vastplan/seed","trustFile":"/etc/trust","tlsCertFile":"/etc/cert","tlsKeyFile":"/etc/key","readTokenFile":"/etc/read","publishTokenFile":"/etc/publish","extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(profile); err == nil {
		t.Fatal("未知 Profile 字段必须拒绝")
	}
}

func TestLoadProfileAcceptsNestedYAML(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("$include: profile-body.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile-body.yaml"), []byte("version: 1\nid: seed-repository\nlisten: 127.0.0.1:8443\nrepositoryRoot: /var/lib/vastplan/seed\ntrustFile: /etc/vastplan/trust.json\ntlsCertFile: /etc/vastplan/tls.crt\ntlsKeyFile: /etc/vastplan/tls.key\nreadTokenFile: /etc/vastplan/read.token\npublishTokenFile: /etc/vastplan/publish.token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProfile(profile)
	if err != nil || loaded.RepositoryRoot != "/var/lib/vastplan/seed" {
		t.Fatalf("嵌套 YAML Seed Profile 未加载: %+v %v", loaded, err)
	}
}

func TestReadPrivateSecretRequiresOwnerOnlyFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("read-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if value, err := readPrivateSecret(file); err != nil || value != "read-token" {
		t.Fatalf("读取私有令牌失败: value=%q err=%v", value, err)
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateSecret(file); err == nil {
		t.Fatal("group/other 可读令牌必须拒绝")
	}
}

func TestEnsurePrivateDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "seed")
	if err := ensurePrivateDirectory(directory); err != nil {
		t.Fatalf("应创建私有 Seed 存储目录: %v", err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(directory); err == nil {
		t.Fatal("group/other 可访问的 Seed 存储目录必须拒绝")
	}
}
