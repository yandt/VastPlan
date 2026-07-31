// dynamicgofingerprint 为同一次原生构建的 dynamic-go Host 与首方 Go .so 生成共同 ABI 指纹。
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var dynamicGoPackages = []string{
	"./core/runtimehosts/go-dynamic",
	"./extensions/plugins/cn.vastplan.foundation.security.bootstrap-policy/dynamic",
}

type listedPackage struct {
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	CFiles     []string
	CXXFiles   []string
	MFiles     []string
	HFiles     []string
	FFiles     []string
	SFiles     []string
	SwigFiles  []string
	EmbedFiles []string
	Module     *listedModule
}

type listedModule struct {
	Main bool
	Dir  string
}

func main() {
	root := flag.String("root", ".", "仓库根目录")
	tags := flag.String("tags", "", "与 dynamic-go Host/.so 一致的 build tags")
	flag.Parse()
	fingerprint, err := calculate(*root, *tags)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(fingerprint)
}

func calculate(root, tags string) (string, error) {
	return calculateForPackages(root, tags, dynamicGoPackages)
}

func calculateForPackages(root, tags string, packages []string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("解析仓库真实路径: %w", err)
	}
	if len(packages) == 0 {
		return "", errors.New("dynamic-go ABI 指纹缺少构建包")
	}
	files, err := localDependencyFiles(root, tags, packages)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		files[name] = struct{}{}
	}

	hash := sha256.New()
	write := func(label string, value []byte) {
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", label, len(value))
		_, _ = hash.Write(value)
	}
	write("schema", []byte("dynamic-go-abi-v2"))
	write("go-version", []byte(runtime.Version()))
	write("goos", []byte(runtime.GOOS))
	write("goarch", []byte(runtime.GOARCH))
	write("cgo", []byte(os.Getenv("CGO_ENABLED")))
	write("tags", []byte(tags))
	ordered := make([]string, 0, len(files))
	for path := range files {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, relative := range ordered {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return "", fmt.Errorf("读取 dynamic-go ABI 输入 %s: %w", relative, err)
		}
		write("file:"+relative, raw)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func localDependencyFiles(root, tags string, packages []string) (map[string]struct{}, error) {
	arguments := []string{"list", "-deps", "-json"}
	if strings.TrimSpace(tags) != "" {
		arguments = append(arguments, "-tags", tags)
	}
	arguments = append(arguments, packages...)
	command := exec.Command("go", arguments...)
	command.Dir = root
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("枚举 dynamic-go ABI 依赖: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	files := map[string]struct{}{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	listed := []listedPackage{}
	moduleRoot := root
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		listed = append(listed, pkg)
		if pkg.Module != nil && pkg.Module.Main && pkg.Module.Dir != "" {
			moduleRoot = pkg.Module.Dir
		}
	}
	for _, pkg := range listed {
		relativeDir, err := filepath.Rel(moduleRoot, pkg.Dir)
		if err != nil || relativeDir == ".." || strings.HasPrefix(relativeDir, ".."+string(filepath.Separator)) {
			continue
		}
		for _, group := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.CFiles, pkg.CXXFiles, pkg.MFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.EmbedFiles} {
			for _, name := range group {
				files[filepath.ToSlash(filepath.Join(relativeDir, name))] = struct{}{}
			}
		}
	}
	if len(files) == 0 {
		return nil, errors.New("dynamic-go ABI 依赖闭包为空")
	}
	return files, nil
}
