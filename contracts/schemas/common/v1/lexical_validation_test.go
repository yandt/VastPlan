package commonv1

import (
	"strings"
	"testing"
)

func TestIsSHA256(t *testing.T) {
	t.Parallel()
	valid := strings.Repeat("a1", 32)
	if !IsSHA256(valid) {
		t.Fatal("合法 SHA-256 应通过")
	}
	for _, value := range []string{"", valid[:63], strings.ToUpper(valid), strings.Repeat("z", 64)} {
		if IsSHA256(value) {
			t.Errorf("非法 SHA-256 不应通过: %q", value)
		}
	}
}

func TestIsHex(t *testing.T) {
	t.Parallel()
	if !IsHex("aA09", 4) {
		t.Fatal("合法十六进制值应通过")
	}
	for _, value := range []string{"", "aA0", "zzzz"} {
		if IsHex(value, 4) {
			t.Errorf("非法十六进制值不应通过: %q", value)
		}
	}
}

func TestIsLowerHex(t *testing.T) {
	t.Parallel()
	if !IsLowerHex("a109", 4) {
		t.Fatal("合法小写十六进制值应通过")
	}
	for _, value := range []string{"", "a10", "aA09", "zzzz"} {
		if IsLowerHex(value, 4) {
			t.Errorf("非法小写十六进制值不应通过: %q", value)
		}
	}
}

func TestIsPrefixedHex(t *testing.T) {
	t.Parallel()
	if !IsPrefixedHex("cfg_aA09", "cfg_", 4) {
		t.Fatal("合法带前缀十六进制 ID 应通过")
	}
	for _, value := range []string{"cfg_aA0", "other_aA09", "cfg_zzzz"} {
		if IsPrefixedHex(value, "cfg_", 4) {
			t.Errorf("非法带前缀十六进制 ID 不应通过: %q", value)
		}
	}
}

func TestIsPrefixedLowerHex(t *testing.T) {
	t.Parallel()
	if !IsPrefixedLowerHex("pcfg_a109", "pcfg_", 4) {
		t.Fatal("合法带前缀小写十六进制 ID 应通过")
	}
	for _, value := range []string{"pcfg_aA09", "pcfg_a10", "other_a109", "pcfg_zzzz"} {
		if IsPrefixedLowerHex(value, "pcfg_", 4) {
			t.Errorf("非法带前缀小写十六进制 ID 不应通过: %q", value)
		}
	}
}
