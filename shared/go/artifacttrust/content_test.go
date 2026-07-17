package artifacttrust

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"
	"testing"
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
