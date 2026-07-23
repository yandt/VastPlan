package artifacttrust

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestInspectPackageRejectsNormalizedDuplicatePaths(t *testing.T) {
	raw := testArchive(t, []tar.Header{
		{Name: manifestName, Mode: 0o644, Size: 2},
		{Name: "dir/../" + manifestName, Mode: 0o644, Size: 2},
	}, []byte("{}"))
	if _, _, err := InspectPackage(raw); err == nil || !strings.Contains(err.Error(), "重复路径") {
		t.Fatalf("规范化后的重复路径必须被拒绝: %v", err)
	}
}

func TestInspectPackageRejectsTooManyFilesBeforeInstallation(t *testing.T) {
	headers := make([]tar.Header, 0, DefaultMaxPackageFiles+1)
	for index := 0; index <= DefaultMaxPackageFiles; index++ {
		headers = append(headers, tar.Header{Name: fmt.Sprintf("files/%05d", index), Mode: 0o644})
	}
	raw := testArchive(t, headers, nil)
	if _, _, err := InspectPackage(raw); err == nil || !strings.Contains(err.Error(), "文件数超过上限") {
		t.Fatalf("信任验证阶段必须限制文件数量: %v", err)
	}
}

func TestReadPackageFileReturnsOnlyBoundedRegularEntry(t *testing.T) {
	module := []byte("export default { register() {} };")
	raw := testArchive(t, []tar.Header{{Name: "frontend/index.js", Mode: 0o644, Size: int64(len(module))}}, module)
	got, err := ReadPackageFile(raw, "frontend/index.js", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, module) {
		t.Fatalf("模块内容不一致: %q", got)
	}
	if _, err := ReadPackageFile(raw, "../frontend/index.js", 1024); err == nil {
		t.Fatal("逃逸路径必须拒绝")
	}
	if _, err := ReadPackageFile(raw, "frontend/index.js", 4); err == nil {
		t.Fatal("超限模块必须拒绝")
	}
}

func TestSameJSONAcceptsEquivalentSafeEscapingAndRejectsChanges(t *testing.T) {
	plain := []byte(`{"requirements":{"node":">=20"},"enabled":true}`)
	escaped := []byte(`{"enabled":true,"requirements":{"node":"\u003e=20"}}`)
	changed := []byte(`{"enabled":true,"requirements":{"node":"\u003e=22"}}`)
	if !sameJSON(plain, escaped) {
		t.Fatal("对象顺序和 HTML 安全转义差异不应改变 JSON 语义")
	}
	if sameJSON(plain, changed) {
		t.Fatal("Manifest 字段变化必须被拒绝")
	}
}

func TestInspectPackageBindsFrontendGraphToActualBytes(t *testing.T) {
	module := []byte("export const ready = true;\n")
	digest := sha256.Sum256(module)
	graph := pluginv1.FrontendModuleGraph{SchemaVersion: "v1", Target: "browser", Entry: "frontend/dist/index.js", Externals: []string{}, Nodes: []pluginv1.FrontendModuleNode{{
		Path: "frontend/dist/index.js", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(module)), MediaType: "text/javascript", Purpose: "entry", Dependencies: []pluginv1.FrontendModuleDependency{},
	}}}
	graph.Digest = graph.ComputedDigest()
	manifest, err := json.Marshal(pluginv1.Manifest{
		ID: "cn.vastplan.product.test.graph", Name: "graph", Description: "graph", Version: "1.0.0", Publisher: "vastplan",
		Engines: map[string]string{"frontend": "^1.0"}, Activation: []string{"onPortalStartup"}, Entry: map[string]string{"frontend": graph.Entry},
		FrontendModuleGraphs: &pluginv1.FrontendModuleGraphs{Browser: &graph}, Contributes: map[string]json.RawMessage{"frontend": json.RawMessage(`{"views":[]}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := testArchiveFiles(t, map[string][]byte{manifestName: manifest, graph.Entry: module})
	if _, _, err := InspectPackage(valid); err != nil {
		t.Fatalf("真实字节与签名图匹配时应通过: %v", err)
	}
	tampered := testArchiveFiles(t, map[string][]byte{manifestName: manifest, graph.Entry: []byte("export const ready = false;\n")})
	if _, _, err := InspectPackage(tampered); err == nil || !strings.Contains(err.Error(), "失配") {
		t.Fatalf("Module Graph 节点被替换必须拒绝: %v", err)
	}
}

func testArchive(t *testing.T, headers []tar.Header, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for _, header := range headers {
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tw.Write(body[:header.Size]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testArchiveFiles(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
