package pluginlibrarysource

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
)

var sourceRoots = []string{filepath.Join("extensions", "plugins"), filepath.Join("examples", "plugins")}

type Observation struct {
	SourceID    string
	Fingerprint string
	Spec        plugindev.Spec
	Err         error
}

func Scan(repositoryRoot string) (map[string]Observation, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, err
	}
	result := map[string]Observation{}
	for _, relativeRoot := range sourceRoots {
		directory := filepath.Join(root, relativeRoot)
		entries, err := os.ReadDir(directory)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			sourceID := filepath.ToSlash(filepath.Join(relativeRoot, entry.Name()))
			fingerprint, fingerprintErr := sourceFingerprint(filepath.Join(directory, entry.Name()))
			spec, discoverErr := plugindev.Discover(root, sourceID)
			observation := Observation{SourceID: sourceID, Fingerprint: fingerprint, Spec: spec}
			if fingerprintErr != nil {
				observation.Err = fingerprintErr
			} else if discoverErr != nil {
				observation.Err = discoverErr
			}
			result[sourceID] = observation
		}
	}
	return result, nil
}

func sourceFingerprint(root string) (string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".vastplan", "node_modules", "dist", "__pycache__", "graphify-out":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("开发插件目录不允许符号链接: %s", path)
		}
		if entry.Type().IsRegular() && !ignoredFile(entry.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	hash := sha256.New()
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		relative, _ := filepath.Rel(root, path)
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%d\x00%d\n", filepath.ToSlash(relative), info.Size(), info.ModTime().UnixNano(), info.Mode().Perm())
		if filepath.Base(path) == "vastplan.plugin.json" {
			file, err := os.Open(path)
			if err != nil {
				return "", err
			}
			_, copyErr := io.Copy(hash, io.LimitReader(file, 2<<20))
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				return "", fmt.Errorf("读取插件 Manifest 指纹: %w", errors.Join(copyErr, closeErr))
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ignoredFile(name string) bool {
	return name == ".DS_Store" || strings.HasSuffix(name, ".pyc") || strings.HasSuffix(name, ".pyo")
}
