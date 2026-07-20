// Package seedrepositorycommand starts the minimal repository bootstrap
// service from a root-owned profile. It intentionally does not load plugins,
// resolve a Platform Profile or contact a control plane.
package seedrepositorycommand

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	artifactservercommand "cdsoft.com.cn/VastPlan/core/kernels/backend/commands/artifactserver"
	"cdsoft.com.cn/VastPlan/core/shared/go/configfile"
)

const ProfileVersion = 1

// Profile is deliberately filesystem-oriented: it is installed and owned by
// the deployment administrator before VastPlan can download any plugin. It
// contains secret file *paths* only, never tokens, keys or trust contents.
type Profile struct {
	Version          int    `json:"version"`
	ID               string `json:"id"`
	Listen           string `json:"listen"`
	RepositoryRoot   string `json:"repositoryRoot"`
	TrustFile        string `json:"trustFile"`
	TLSCertFile      string `json:"tlsCertFile"`
	TLSKeyFile       string `json:"tlsKeyFile"`
	ReadTokenFile    string `json:"readTokenFile"`
	PublishTokenFile string `json:"publishTokenFile"`
}

func LoadProfile(filename string) (Profile, error) {
	raw, err := configfile.Load(filename)
	if err != nil {
		return Profile{}, fmt.Errorf("读取 Seed 仓库 Profile: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("解析 Seed 仓库 Profile: %w", err)
	}
	if err := ValidateProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func ValidateProfile(profile Profile) error {
	if profile.Version != ProfileVersion || profile.ID != "seed-repository" || strings.TrimSpace(profile.Listen) == "" {
		return errors.New("Seed 仓库 Profile 的 version、id 或 listen 无效")
	}
	for _, item := range []struct{ label, path string }{
		{"repositoryRoot", profile.RepositoryRoot}, {"trustFile", profile.TrustFile}, {"tlsCertFile", profile.TLSCertFile},
		{"tlsKeyFile", profile.TLSKeyFile}, {"readTokenFile", profile.ReadTokenFile}, {"publishTokenFile", profile.PublishTokenFile},
	} {
		if !absoluteClean(item.path) {
			return fmt.Errorf("Seed 仓库 Profile %s 必须是规范绝对路径", item.label)
		}
	}
	if profile.ReadTokenFile == profile.PublishTokenFile {
		return errors.New("Seed 仓库读写令牌必须来自不同文件")
	}
	return nil
}

func Run(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("seed-artifact-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profilePath := flags.String("profile", "", "root-owned Seed 仓库 Profile（JSON/YAML）")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *profilePath == "" {
		flags.Usage()
		return errors.New("必须提供 -profile")
	}
	profile, err := LoadProfile(*profilePath)
	if err != nil {
		return err
	}
	if err := ensurePrivateDirectory(profile.RepositoryRoot); err != nil {
		return fmt.Errorf("Seed 本地制品存储: %w", err)
	}
	if err := ensurePrivateFile(profile.TLSKeyFile); err != nil {
		return fmt.Errorf("Seed TLS 私钥: %w", err)
	}
	readToken, err := readPrivateSecret(profile.ReadTokenFile)
	if err != nil {
		return fmt.Errorf("Seed 读令牌: %w", err)
	}
	publishToken, err := readPrivateSecret(profile.PublishTokenFile)
	if err != nil {
		return fmt.Errorf("Seed 发布令牌: %w", err)
	}
	return artifactservercommand.RunConfig(ctx, artifactservercommand.Config{
		Addr: profile.Listen, Repository: profile.RepositoryRoot, TrustFile: profile.TrustFile,
		TLSCertFile: profile.TLSCertFile, TLSKeyFile: profile.TLSKeyFile,
		ReadToken: readToken, PublishToken: publishToken,
	}, stderr)
}

func absoluteClean(value string) bool { return filepath.IsAbs(value) && filepath.Clean(value) == value }

func ensurePrivateFile(filename string) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("必须是非符号链接且仅属主可读写的普通文件")
	}
	return nil
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("必须是非符号链接且仅属主可访问的目录")
	}
	return nil
}

func readPrivateSecret(filename string) (string, error) {
	if err := ensurePrivateFile(filename); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > 16<<10 {
		return "", errors.New("令牌为空或超过大小上限")
	}
	return value, nil
}
