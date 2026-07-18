package main

import (
	"crypto/sha256"
	"testing"
)

func TestDeterministicUUIDIsRFC4122Version5(t *testing.T) {
	digest := sha256.Sum256([]byte("backend-kernel"))
	first := deterministicUUID(digest)
	second := deterministicUUID(digest)
	if first != second {
		t.Fatalf("同一摘要必须产生同一 UUID: %q != %q", first, second)
	}
	if len(first) != 36 || first[14] != '5' {
		t.Fatalf("不是 version 5 UUID: %q", first)
	}
	if first[19] != '8' && first[19] != '9' && first[19] != 'a' && first[19] != 'b' {
		t.Fatalf("不是 RFC 4122 variant: %q", first)
	}
}
