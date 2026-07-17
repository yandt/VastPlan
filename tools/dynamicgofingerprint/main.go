// dynamicgofingerprint 为同一次原生构建的 Backend 与首方 Go .so 生成共同指纹。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func main() {
	root := flag.String("root", ".", "仓库根目录")
	tags := flag.String("tags", "", "与 Backend/.so 一致的 build tags")
	flag.Parse()
	fingerprint, err := calculate(*root, *tags)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(fingerprint)
}

func calculate(root, tags string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	write := func(label string, value []byte) {
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", label, len(value))
		_, _ = hash.Write(value)
	}
	write("go-version", []byte(runtime.Version()))
	write("goos", []byte(runtime.GOOS))
	write("goarch", []byte(runtime.GOARCH))
	write("cgo", []byte(os.Getenv("CGO_ENABLED")))
	write("tags", []byte(tags))
	for _, name := range []string{"go.mod", "go.sum"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return "", fmt.Errorf("读取 %s: %w", name, err)
		}
		write(name, raw)
	}
	if revision, err := git(root, "rev-parse", "HEAD"); err == nil {
		write("git-revision", revision)
	}
	if diff, err := git(root, "diff", "--binary", "HEAD", "--", "."); err == nil {
		write("git-diff", diff)
	}
	if untracked, err := git(root, "ls-files", "--others", "--exclude-standard"); err == nil {
		paths := strings.Fields(string(untracked))
		sort.Strings(paths)
		for _, path := range paths {
			raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if readErr != nil {
				return "", fmt.Errorf("读取未跟踪构建输入 %s: %w", path, readErr)
			}
			write("untracked:"+path, raw)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func git(root string, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = root
	return command.Output()
}
