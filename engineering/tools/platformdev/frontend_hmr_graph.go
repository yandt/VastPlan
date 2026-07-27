package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type frontendHMRObject struct {
	Bytes     []byte
	MediaType string
}

type frontendHMRGraph struct {
	Target    string                 `json:"target"`
	Entry     string                 `json:"entry"`
	Digest    string                 `json:"digest"`
	Externals []string               `json:"externals"`
	Nodes     []frontendHMRGraphNode `json:"nodes"`
}

type frontendHMRGraphNode struct {
	Path         string                       `json:"path"`
	URL          string                       `json:"url,omitempty"`
	SHA256       string                       `json:"sha256"`
	Size         int64                        `json:"size"`
	MediaType    string                       `json:"mediaType"`
	Purpose      string                       `json:"purpose"`
	Dependencies []frontendHMRGraphDependency `json:"dependencies"`
}

type frontendHMRGraphDependency struct {
	Specifier string `json:"specifier"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
}

func loadFrontendHMRGraph(directory, pluginID, entry, entrySHA string, graph frontendHMRGraph) (frontendHMRGraph, map[string]frontendHMRObject, error) {
	if graph.Target != "browser" || graph.Entry != entry || !sha256Value.MatchString(graph.Digest) || len(graph.Nodes) == 0 || len(graph.Nodes) > 512 || len(graph.Externals) > 32 {
		return frontendHMRGraph{}, nil, errors.New("图结构或入口无效")
	}
	pluginRoot := filepath.Join(directory, pluginID)
	objects := make(map[string]frontendHMRObject, len(graph.Nodes))
	paths := map[string]struct{}{}
	digests := map[string]struct{}{}
	var totalSize int64
	entryFound := false
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		cleanPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(node.Path)))
		if node.Path == "" || filepath.IsAbs(node.Path) || cleanPath != node.Path || strings.HasPrefix(node.Path, "../") || !sha256Value.MatchString(node.SHA256) || node.Size <= 0 || node.Size > 16<<20 || len(node.Dependencies) > 128 {
			return frontendHMRGraph{}, nil, fmt.Errorf("节点字段无效: %s", node.Path)
		}
		totalSize += node.Size
		if totalSize > 64<<20 {
			return frontendHMRGraph{}, nil, errors.New("图节点总大小超过 64 MiB")
		}
		absolute := filepath.Join(pluginRoot, filepath.FromSlash(node.Path))
		relative, err := filepath.Rel(pluginRoot, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return frontendHMRGraph{}, nil, fmt.Errorf("节点路径逃逸候选目录: %s", node.Path)
		}
		if _, exists := paths[node.Path]; exists {
			return frontendHMRGraph{}, nil, fmt.Errorf("节点路径重复: %s", node.Path)
		}
		if _, exists := digests[node.SHA256]; exists {
			return frontendHMRGraph{}, nil, fmt.Errorf("节点摘要重复: %s", node.SHA256)
		}
		paths[node.Path] = struct{}{}
		digests[node.SHA256] = struct{}{}
		content, err := os.ReadFile(absolute)
		if err != nil {
			return frontendHMRGraph{}, nil, fmt.Errorf("读取节点 %s: %w", node.Path, err)
		}
		digest := sha256.Sum256(content)
		actual := hex.EncodeToString(digest[:])
		if int64(len(content)) != node.Size || actual != node.SHA256 {
			return frontendHMRGraph{}, nil, fmt.Errorf("节点摘要或大小漂移: %s", node.Path)
		}
		extension, ok := developmentModuleExtension(node.MediaType)
		if !ok {
			return frontendHMRGraph{}, nil, fmt.Errorf("节点媒体类型不受支持: %s", node.MediaType)
		}
		node.URL = "/__vastplan_dev/modules/" + actual + extension
		if err := addFrontendHMRObject(objects, actual, frontendHMRObject{Bytes: append([]byte(nil), content...), MediaType: node.MediaType}); err != nil {
			return frontendHMRGraph{}, nil, err
		}
		if node.Path == entry {
			entryFound = node.Purpose == "entry" && actual == entrySHA
		}
	}
	if !entryFound {
		return frontendHMRGraph{}, nil, errors.New("图入口与候选入口文件不一致")
	}
	for _, node := range graph.Nodes {
		for _, dependency := range node.Dependencies {
			if _, exists := paths[dependency.Path]; !exists || dependency.Path == node.Path {
				return frontendHMRGraph{}, nil, fmt.Errorf("节点依赖未闭合: %s -> %s", node.Path, dependency.Path)
			}
		}
	}
	return graph, objects, nil
}

func developmentModuleExtension(mediaType string) (string, bool) {
	switch mediaType {
	case "text/javascript":
		return ".js", true
	case "text/css":
		return ".css", true
	case "application/json":
		return ".json", true
	case "application/wasm":
		return ".wasm", true
	case "application/octet-stream", "image/svg+xml", "font/woff2":
		return ".bin", true
	default:
		return "", false
	}
}

func addFrontendHMRObject(objects map[string]frontendHMRObject, digest string, object frontendHMRObject) error {
	if existing, ok := objects[digest]; ok {
		if existing.MediaType != object.MediaType || !bytes.Equal(existing.Bytes, object.Bytes) {
			return fmt.Errorf("摘要 %s 绑定了不同内容或媒体类型", digest)
		}
		return nil
	}
	objects[digest] = object
	return nil
}
