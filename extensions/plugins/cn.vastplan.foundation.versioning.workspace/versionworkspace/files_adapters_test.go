package versionworkspace

import (
	"context"
	"reflect"
	"strings"
	"testing"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

func manifest(entries ...resourcev1.FileEntry) resourcev1.Snapshot {
	return resourcev1.Snapshot{Kind: resourcev1.ContentFiles, MediaType: resourcev1.FilesManifestMediaType, Files: entries}
}

func fileEntry(path, mediaType, digest string, size int64) resourcev1.FileEntry {
	return resourcev1.FileEntry{Path: path, MediaType: mediaType, Digest: digest, Size: size, Mode: 0o644}
}

func TestStandardFilesAdaptersNormalizeShapesAndLargeContentReferences(t *testing.T) {
	resource := resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"}
	text := NewTextAdapter()
	request := resourcev1.AdapterNormalizeRequest{Resource: resource, Mode: resourcev1.ModeSnapshot, Snapshot: manifest()}
	if _, err := text.Normalize(context.Background(), request); err != nil {
		t.Fatalf("新 Text 资源应允许空清单: %v", err)
	}
	request.Snapshot = manifest(fileEntry("main.ts", "text/typescript", strings.Repeat("a", 64), 1<<30))
	if _, err := text.Normalize(context.Background(), request); err != nil {
		t.Fatalf("大文件只以清单引用时不应受 Wire 大小误伤: %v", err)
	}
	request.Snapshot.Files[0].MediaType = "application/octet-stream"
	if _, err := text.Normalize(context.Background(), request); err == nil {
		t.Fatal("Text Adapter 必须拒绝非文本 mediaType")
	}
	blob := NewBlobAdapter()
	request.Snapshot = manifest(
		fileEntry("a.bin", "application/octet-stream", strings.Repeat("a", 64), 1),
		fileEntry("b.bin", "application/octet-stream", strings.Repeat("b", 64), 1),
	)
	if _, err := blob.Normalize(context.Background(), request); err == nil {
		t.Fatal("Blob Adapter 不得接受多个文件")
	}
}

func TestFilesAdapterProducesDeterministicManifestDiff(t *testing.T) {
	adapter := NewFilesAdapter()
	resource := resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"}
	left := manifest(
		fileEntry("README.md", "text/markdown", strings.Repeat("a", 64), 10),
		fileEntry("main.ts", "text/typescript", strings.Repeat("b", 64), 20),
	)
	right := manifest(
		fileEntry("main.ts", "text/typescript", strings.Repeat("c", 64), 21),
		fileEntry("test.ts", "text/typescript", strings.Repeat("d", 64), 30),
	)
	diff, err := adapter.Diff(context.Background(), resourcev1.AdapterDiffRequest{Resource: resource, Mode: resourcev1.ModeSnapshot, Left: left, Right: right})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diff.ChangedPaths, []string{"README.md", "main.ts", "test.ts"}) || diff.Summary.Added != 1 || diff.Summary.Modified != 1 || diff.Summary.Removed != 1 {
		t.Fatalf("文件 diff 无效: %+v", diff)
	}
}
