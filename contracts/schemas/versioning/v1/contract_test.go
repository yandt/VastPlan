package versioningv1_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

var fixedTime = time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)

func TestCanonicalGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/canonical-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name      string `json:"name"`
		Input     string `json:"input"`
		Canonical string `json:"canonical"`
		SHA256    string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			canonical, err := versioningv1.CanonicalizeContent(json.RawMessage(vector.Input))
			if err != nil {
				t.Fatal(err)
			}
			if string(canonical) != vector.Canonical {
				t.Fatalf("规范输出漂移:\nwant %s\n got %s", vector.Canonical, canonical)
			}
			digest, err := versioningv1.ContentDigest(canonical)
			if err != nil {
				t.Fatal(err)
			}
			if digest != vector.SHA256 {
				t.Fatalf("摘要漂移: want %s, got %s", vector.SHA256, digest)
			}
		})
	}
}

func TestContentCanonicalizationIsStableAndBounded(t *testing.T) {
	first, err := versioningv1.CanonicalizeContent(json.RawMessage(`{"z":2,"nested":{"b":true,"a":null},"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := versioningv1.CanonicalizeContent(json.RawMessage(` { "a": 1, "nested": { "a": null, "b": true }, "z": 2 } `))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("同一 JSON 内容必须得到相同规范表示:\n%s\n%s", first, second)
	}
	digestA, err := versioningv1.ContentDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := versioningv1.ContentDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if digestA != digestB || len(digestA) != 64 {
		t.Fatalf("规范内容摘要不稳定: %q %q", digestA, digestB)
	}

	for name, raw := range map[string]json.RawMessage{
		"非 object 根": json.RawMessage(`[1,2,3]`),
		"重复 key":     json.RawMessage(`{"safe":true,"safe":false}`),
		"尾随文档":       json.RawMessage(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := versioningv1.CanonicalizeContent(raw); err == nil {
				t.Fatalf("非法内容必须被拒绝: %s", raw)
			}
		})
	}
	oversized := json.RawMessage(`{"value":"` + strings.Repeat("x", versioningv1.MaxContentBytes) + `"}`)
	if _, err := versioningv1.CanonicalizeContent(oversized); err == nil {
		t.Fatal("超过内容上限的版本必须被拒绝")
	}
}

func TestPublicRequestParserNormalizesContentAndRejectsAmbiguity(t *testing.T) {
	request := versioningv1.PutVersionRequest{
		Stream:         stream("portal.configuration", "portal-main"),
		IdempotencyKey: "portal-main:revision:0001",
		Content:        json.RawMessage(`{"b":2,"a":1}`),
		Message:        "initial version",
	}
	parsed, err := versioningv1.ParseRequest(versioningv1.OperationPutVersion, marshal(t, request))
	if err != nil {
		t.Fatal(err)
	}
	put := parsed.(*versioningv1.PutVersionRequest)
	if string(put.Content) != `{"a":1,"b":2}` {
		t.Fatalf("Ledger 必须在进入 Provider 前规范内容: %s", put.Content)
	}

	unknown := `{"stream":{"namespace":"portal.configuration","streamId":"portal-main"},"idempotencyKey":"portal-main:revision:0001","content":{},"providerId":"attacker"}`
	if _, err := versioningv1.ParseRequest(versioningv1.OperationPutVersion, []byte(unknown)); err == nil {
		t.Fatal("调用方不得通过未知字段选择 Provider")
	}
	duplicate := `{"stream":{"namespace":"portal.configuration","streamId":"portal-main"},"idempotencyKey":"portal-main:revision:0001","content":{"mode":"safe","mode":"unsafe"}}`
	if _, err := versioningv1.ParseRequest(versioningv1.OperationPutVersion, []byte(duplicate)); err == nil {
		t.Fatal("嵌套重复 JSON key 必须被拒绝")
	}
	if _, err := versioningv1.ParseRequest("deleteVersion", []byte(`{}`)); err == nil {
		t.Fatal("v1 不得提供可变或删除版本操作")
	}
}

func TestVersionRecordDigestParentAndHistoryAreFailClosed(t *testing.T) {
	root := record(t, 1, nil, `{"layout":"standard"}`)
	child := record(t, 2, &root.Ref, `{"layout":"top-navigation"}`)
	if err := versioningv1.ValidateVersionRecord(child); err != nil {
		t.Fatalf("有效版本被拒绝: %v", err)
	}
	if err := versioningv1.ValidateHistory([]versioningv1.VersionRecord{child, root}); err != nil {
		t.Fatalf("有效父链被拒绝: %v", err)
	}

	tampered := child
	tampered.Content = json.RawMessage(`{"layout":"attacker"}`)
	if err := versioningv1.ValidateVersionRecord(tampered); err == nil {
		t.Fatal("内容摘要不匹配必须 fail-closed")
	}
	broken := child
	wrongParent := root.Ref
	wrongParent.VersionID = strings.Repeat("c", 64)
	broken.Parent = &wrongParent
	if err := versioningv1.ValidateHistory([]versioningv1.VersionRecord{broken, root}); err == nil {
		t.Fatal("不闭合的父链必须被拒绝")
	}
}

func TestHeadCASContractKeepsTargetInTheSameStream(t *testing.T) {
	root := record(t, 1, nil, `{"layout":"standard"}`)
	request := versioningv1.MoveHeadRequest{
		Stream: root.Ref.Stream, Name: "published", Target: root.Ref, ExpectedRevision: 0,
	}
	if _, err := versioningv1.ParseRequest(versioningv1.OperationMoveHead, marshal(t, request)); err != nil {
		t.Fatalf("有效首次 Head CAS 被拒绝: %v", err)
	}
	request.Target.Stream = stream("portal.configuration", "another-portal")
	if _, err := versioningv1.ParseRequest(versioningv1.OperationMoveHead, marshal(t, request)); err == nil {
		t.Fatal("Head 不得指向其他 stream")
	}

	head := versioningv1.Head{
		Protocol: versioningv1.Protocol, Stream: root.Ref.Stream, Name: "published",
		Target: root.Ref, Revision: 1, UpdatedAt: fixedTime,
	}
	if _, err := versioningv1.ParseResult(versioningv1.OperationMoveHead, marshal(t, versioningv1.MoveHeadResult{Head: head})); err != nil {
		t.Fatalf("有效 Head 结果被拒绝: %v", err)
	}
}

func TestProviderDescriptorsDeclareHonestStorageSemantics(t *testing.T) {
	file := provider(versioningv1.StorageProtocolFile)
	if err := versioningv1.ValidateProviderDescriptor(file); err != nil {
		t.Fatalf("有效 File Provider 被拒绝: %v", err)
	}
	file.ClusterSafe = true
	if err := versioningv1.ValidateProviderDescriptor(file); err == nil {
		t.Fatal("File Provider 不得虚假声明 clusterSafe")
	}

	git := provider(versioningv1.StorageProtocolGit)
	git.Consistency = versioningv1.ConsistencySingleWriter
	if err := versioningv1.ValidateProviderDescriptor(git); err == nil {
		t.Fatal("Git Provider 必须声明 ref-CAS")
	}

	relational := provider(versioningv1.StorageProtocolRelational)
	if err := versioningv1.ValidateProviderDescriptor(relational); err != nil {
		t.Fatalf("有效 Relational Provider 被拒绝: %v", err)
	}
	relational.ConfigurationSchema = json.RawMessage(`{"type":"object","properties":{"credential":{"$ref":"https://attacker.invalid/schema.json"}}}`)
	if err := versioningv1.ValidateProviderDescriptor(relational); err == nil {
		t.Fatal("Provider 配置不得引用外部 Schema")
	}
}

func TestProviderSPIAcceptsOnlyLedgerAssignedRecords(t *testing.T) {
	root := record(t, 1, nil, `{"layout":"standard"}`)
	request := versioningv1.ProviderPutVersionRequest{
		IdempotencyKey: "portal-main:revision:0001",
		Version:        root,
	}
	parsed, err := versioningv1.ParseProviderRequest(versioningv1.ProviderOperationPutVersion, marshal(t, request))
	if err != nil {
		t.Fatalf("有效 Provider 写入被拒绝: %v", err)
	}
	if parsed.(*versioningv1.ProviderPutVersionRequest).Version.ActorID != root.ActorID {
		t.Fatal("Provider SPI 丢失 Ledger 已赋予的可信 actor")
	}

	request.Version.Ref.ContentDigest = strings.Repeat("0", 64)
	if _, err := versioningv1.ParseProviderRequest(versioningv1.ProviderOperationPutVersion, marshal(t, request)); err == nil {
		t.Fatal("Provider 必须复核 Ledger 记录摘要")
	}
	describe := versioningv1.ProviderDescribeResult{Provider: provider(versioningv1.StorageProtocolRelational)}
	if _, err := versioningv1.ParseProviderResult(versioningv1.ProviderOperationDescribe, marshal(t, describe)); err != nil {
		t.Fatalf("有效 Provider 描述被拒绝: %v", err)
	}
}

func TestStableErrorCodesAreRegistered(t *testing.T) {
	for _, code := range []string{
		versioningv1.ErrorInvalidRequest, versioningv1.ErrorProviderNotFound,
		versioningv1.ErrorProviderUnavailable, versioningv1.ErrorNotFound,
		versioningv1.ErrorConflict, versioningv1.ErrorDigestMismatch,
		versioningv1.ErrorCorrupted, versioningv1.ErrorLimitExceeded,
		versioningv1.ErrorUnsupported,
	} {
		if !versioningv1.KnownErrorCode(code) {
			t.Fatalf("稳定错误码未登记: %s", code)
		}
	}
	if versioningv1.KnownErrorCode("version.ledger.internal_detail") {
		t.Fatal("内部错误细节不得被误登记为稳定协议错误码")
	}
}

func stream(namespace, streamID string) versioningv1.StreamKey {
	return versioningv1.StreamKey{Namespace: namespace, StreamID: streamID}
}

func record(t *testing.T, sequence uint64, parent *versioningv1.VersionRef, content string) versioningv1.VersionRecord {
	t.Helper()
	digest, err := versioningv1.ContentDigest(json.RawMessage(content))
	if err != nil {
		t.Fatal(err)
	}
	versionIDSeed := "a"
	if sequence > 1 {
		versionIDSeed = "b"
	}
	return versioningv1.VersionRecord{
		Protocol: versioningv1.Protocol,
		Ref: versioningv1.VersionRef{
			Stream: stream("portal.configuration", "portal-main"), VersionID: strings.Repeat(versionIDSeed, 64),
			Sequence: sequence, ContentDigest: digest,
		},
		Parent: parent, Content: json.RawMessage(content), ActorID: "user:platform-admin", CreatedAt: fixedTime,
	}
}

func provider(protocol string) versioningv1.ProviderDescriptor {
	descriptor := versioningv1.ProviderDescriptor{
		ID: "local-file", Protocol: protocol, Version: "1.0.0", DisplayName: "Local file",
		Consistency: versioningv1.ConsistencySingleWriter, Durability: versioningv1.DurabilityLocal,
		MaxContentBytes:     versioningv1.MaxContentBytes,
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		Capabilities:        versioningv1.ProviderCapabilities{DetachedVersions: true, NamedHeads: true, StableHistory: true},
	}
	switch protocol {
	case versioningv1.StorageProtocolGit:
		descriptor.ID = "git"
		descriptor.DisplayName = "Git"
		descriptor.Consistency = versioningv1.ConsistencyRefCAS
		descriptor.Durability = versioningv1.DurabilityShared
		descriptor.ClusterSafe = true
	case versioningv1.StorageProtocolRelational:
		descriptor.ID = "relational"
		descriptor.DisplayName = "Relational database"
		descriptor.Consistency = versioningv1.ConsistencyLinearizable
		descriptor.Durability = versioningv1.DurabilityShared
		descriptor.ClusterSafe = true
	}
	return descriptor
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
