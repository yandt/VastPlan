package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type verifiedFrontendGraph struct {
	Graph pluginv1.FrontendModuleGraph
	root  string
}

func loadVerifiedFrontendGraph(filename, root string, manifest pluginv1.Manifest) *verifiedFrontendGraph {
	raw, err := os.ReadFile(filename)
	if err != nil {
		fatalf("读取 frontend Module Graph 失败: %v", err)
	}
	var graph pluginv1.FrontendModuleGraph
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&graph); err != nil {
		fatalf("frontend Module Graph 格式无效: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fatalf("frontend Module Graph 只能包含一个 JSON 文档")
	}
	contract := &verifiedFrontendGraph{Graph: graph, root: root}
	for _, node := range graph.Nodes {
		filename, pathErr := containedGraphPath(root, node.Path)
		if pathErr != nil {
			fatalf("frontend Module Graph 路径无效: %v", pathErr)
		}
		info, statErr := os.Lstat(filename)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != node.Size {
			fatalf("frontend Module Graph 节点不是匹配的普通文件: %s", node.Path)
		}
		content, readErr := os.ReadFile(filename)
		if readErr != nil {
			fatalf("读取 frontend Module Graph 节点失败: %v", readErr)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != node.SHA256 {
			fatalf("frontend Module Graph 节点摘要失配: %s", node.Path)
		}
	}
	candidate := manifest
	candidate.FrontendModuleGraphs = &pluginv1.FrontendModuleGraphs{Browser: &graph}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		fatalf("编码 frontend Module Graph 候选清单失败: %v", err)
	}
	if _, err := pluginv1.ParseManifest(encoded); err != nil {
		fatalf("frontend Module Graph 不符合签名清单契约: %v", err)
	}
	return contract
}

func (g *verifiedFrontendGraph) CopyTo(staging string) error {
	if g == nil {
		return nil
	}
	for _, node := range g.Graph.Nodes {
		source, err := containedGraphPath(g.root, node.Path)
		if err != nil {
			return err
		}
		target, err := containedGraphPath(staging, node.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(source, target, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func containedGraphPath(root, name string) (string, error) {
	if strings.TrimSpace(root) == "" || name == "" || filepath.IsAbs(name) {
		return "", errors.New("Module Graph 根和节点路径不能为空或为绝对路径")
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Module Graph 节点逃逸根目录: %s", name)
	}
	return filepath.Join(root, clean), nil
}
