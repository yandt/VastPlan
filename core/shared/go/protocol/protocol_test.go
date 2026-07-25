package protocol

import "testing"

// 协议版本取交集里最高的；无交集返回 -1（调用方据此 fail-closed）。
func TestNegotiate(t *testing.T) {
	cases := []struct {
		name string
		a, b []int32
		want int32
	}{
		{"单一交集", []int32{1}, []int32{1}, 1},
		{"多交集取最高", []int32{1, 2, 3}, []int32{2, 3}, 3},
		{"无交集", []int32{1}, []int32{2}, -1},
		{"空集", []int32{}, []int32{1}, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Negotiate(c.a, c.b); got != c.want {
				t.Fatalf("Negotiate(%v,%v) = %d，期望 %d", c.a, c.b, got, c.want)
			}
		})
	}
}

// engines 校验：内核版本须落在插件声明的 SemVer 范围内。
func TestCheckEngine(t *testing.T) {
	cases := []struct {
		name          string
		kernelVersion string
		constraint    string
		wantErr       bool
	}{
		{"满足 caret", "0.1.0", "^0.1", false},
		{"满足 caret 补丁位", "0.1.9", "^0.1", false},
		{"不满足：内核过新", "0.2.0", "^0.1", true},
		{"不满足：内核过旧", "0.0.9", "^0.1", true},
		{"满足 >=", "1.5.0", ">=1.0.0", false},
		{"未声明本内核 → fail-closed 拒绝", "0.1.0", "", true},
		{"非法约束", "0.1.0", "不是版本约束", true},
		{"非法内核版本", "abc", "^0.1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckEngine("backend", c.kernelVersion, c.constraint)
			if c.wantErr && err == nil {
				t.Fatalf("期望拒绝，实际通过（内核 %s，约束 %q）", c.kernelVersion, c.constraint)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("期望通过，实际拒绝: %v", err)
			}
		})
	}
}

func TestSupports(t *testing.T) {
	if !Supports(1) {
		t.Fatal("应支持协议 v1")
	}
	if Supports(99) {
		t.Fatal("不应支持协议 v99")
	}
}

func TestNegotiateFeatures(t *testing.T) {
	got := NegotiateFeatures([]string{FeatureEventPublish, "unknown", FeatureCancellation}, SupportedFeatures)
	if len(got) != 2 || got[0] != FeatureCancellation || got[1] != FeatureEventPublish {
		t.Fatalf("特性协商应只返回交集并保持宿主顺序: %v", got)
	}
	if !HasFeature(got, FeatureCancellation) || HasFeature(got, "unknown") {
		t.Fatalf("特性查询错误: %v", got)
	}
}

func TestResolveHandshakeVersion(t *testing.T) {
	for _, test := range []struct {
		name, declared, projected, channel, want string
		wantErr                                  bool
	}{
		{name: "legacy declaration", declared: "1.2.3", want: "1.2.3"},
		{name: "stable exact projection", declared: "1.2.3", projected: "1.2.3", channel: "stable", want: "1.2.3"},
		{name: "workspace derived", declared: "1.2.3", projected: "1.2.3-dev.workspace.0123456789abcdef", channel: "workspace", want: "1.2.3-dev.workspace.0123456789abcdef"},
		{name: "stable drift", declared: "1.2.3", projected: "1.2.4", channel: "stable", wantErr: true},
		{name: "workspace base drift", declared: "1.2.3", projected: "1.2.4-dev.workspace.0123456789abcdef", channel: "workspace", wantErr: true},
		{name: "workspace arbitrary prerelease", declared: "1.2.3", projected: "1.2.3-dev.manual.1", channel: "workspace", wantErr: true},
		{name: "partial projection", declared: "1.2.3", channel: "workspace", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveHandshakeVersion(test.declared, test.projected, test.channel)
			if test.wantErr && err == nil {
				t.Fatalf("expected error, got %q", got)
			}
			if !test.wantErr && (err != nil || got != test.want) {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestRuntimeDeclaredVersion(t *testing.T) {
	got, err := RuntimeDeclaredVersion("1.2.3-dev.workspace.0123456789abcdef", "workspace")
	if err != nil || got != "1.2.3" {
		t.Fatalf("workspace source version = %q, %v", got, err)
	}
	if got, err := RuntimeDeclaredVersion("1.2.3-dev.7", "testing"); err != nil || got != "1.2.3-dev.7" {
		t.Fatalf("testing version = %q, %v", got, err)
	}
	if _, err := RuntimeDeclaredVersion("1.2.3", "workspace"); err == nil {
		t.Fatal("workspace channel must use derived prerelease")
	}
}
