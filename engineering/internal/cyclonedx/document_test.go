package cyclonedx

import (
	"crypto/sha256"
	"testing"
)

func TestBuildIsDeterministicAndDeduplicatesComponents(t *testing.T) {
	root := Component{Type: "application", BOMRef: "pkg:generic/demo@1.0.0", Name: "demo", Version: "1.0.0", PURL: "pkg:generic/demo@1.0.0"}
	dependency := Component{Type: "library", BOMRef: "pkg:golang/example.org/lib@v1.2.0", Name: "example.org/lib", Version: "v1.2.0", PURL: "pkg:golang/example.org/lib@v1.2.0"}
	first, err := Build(root, []Component{dependency, dependency}, []byte("seed"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(root, []Component{dependency}, []byte("seed"))
	if err != nil {
		t.Fatal(err)
	}
	left, _ := Marshal(first)
	right, _ := Marshal(second)
	if string(left) != string(right) || len(first.Components) != 1 || len(first.Dependencies) != 1 {
		t.Fatalf("CycloneDX 输出不确定: %s != %s", left, right)
	}
}

func TestDeterministicUUIDIsRFC4122Version5(t *testing.T) {
	digest := sha256.Sum256([]byte("backend-kernel"))
	value := DeterministicUUID(digest)
	if len(value) != 36 || value[14] != '5' || (value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b') {
		t.Fatalf("不是 RFC 4122 UUIDv5: %q", value)
	}
}
