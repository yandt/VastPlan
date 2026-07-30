package versioningv1_test

import (
	"encoding/json"
	"testing"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func TestVersionRecordSupportsAtMostTwoDistinctParents(t *testing.T) {
	root := record(t, 1, nil, `{"v":1}`)
	left := record(t, 2, &root.Ref, `{"v":2}`)
	right := record(t, 3, &root.Ref, `{"v":3}`)
	right.Ref.VersionID = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	digest, err := versioningv1.ContentDigest(json.RawMessage(`{"v":4}`))
	if err != nil {
		t.Fatal(err)
	}
	merge := versioningv1.VersionRecord{
		Protocol: versioningv1.Protocol,
		Ref:      versioningv1.VersionRef{Stream: root.Ref.Stream, VersionID: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Sequence: 4, ContentDigest: digest},
		Parents:  []versioningv1.VersionRef{left.Ref, right.Ref}, Content: json.RawMessage(`{"v":4}`), ActorID: "plugin:composer", CreatedAt: fixedTime,
	}
	if err := versioningv1.ValidateVersionRecord(merge); err != nil {
		t.Fatalf("有效双父版本被拒绝: %v", err)
	}
	merge.Parents = append(merge.Parents, root.Ref)
	if err := versioningv1.ValidateVersionRecord(merge); err == nil {
		t.Fatal("超过两个父节点必须拒绝")
	}
	merge.Parents = []versioningv1.VersionRef{left.Ref, left.Ref}
	if err := versioningv1.ValidateVersionRecord(merge); err == nil {
		t.Fatal("重复父节点必须拒绝")
	}
}

func TestGitLikeReferenceContractsAreStrict(t *testing.T) {
	root := record(t, 1, nil, `{}`)
	requests := map[string]any{
		versioningv1.OperationListHeads:  versioningv1.ListHeadsRequest{Stream: root.Ref.Stream, Limit: 20},
		versioningv1.OperationCreateHead: versioningv1.CreateHeadRequest{Stream: root.Ref.Stream, Name: "feature", Target: root.Ref},
		versioningv1.OperationDeleteHead: versioningv1.DeleteHeadRequest{Stream: root.Ref.Stream, Name: "feature", ExpectedRevision: 1},
		versioningv1.OperationCreateTag:  versioningv1.CreateTagRequest{Stream: root.Ref.Stream, Name: "release-1", Target: root.Ref},
		versioningv1.OperationGetTag:     versioningv1.GetTagRequest{Stream: root.Ref.Stream, Name: "release-1"},
		versioningv1.OperationListTags:   versioningv1.ListTagsRequest{Stream: root.Ref.Stream, Limit: 20},
	}
	for operation, request := range requests {
		if _, err := versioningv1.ParseRequest(operation, marshal(t, request)); err != nil {
			t.Fatalf("%s 有效请求被拒绝: %v", operation, err)
		}
	}
	providerTag := versioningv1.ProviderCreateTagRequest{Stream: root.Ref.Stream, Name: "release-1", Target: root.Ref, ActorID: "plugin:composer"}
	if _, err := versioningv1.ParseProviderRequest(versioningv1.ProviderOperationCreateTag, marshal(t, providerTag)); err != nil {
		t.Fatalf("Provider Tag 请求被拒绝: %v", err)
	}
	tag := versioningv1.Tag{Protocol: versioningv1.Protocol, Stream: root.Ref.Stream, Name: "release-1", Target: root.Ref, ActorID: "plugin:composer", CreatedAt: time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)}
	if _, err := versioningv1.ParseResult(versioningv1.OperationCreateTag, marshal(t, versioningv1.CreateTagResult{Tag: tag})); err != nil {
		t.Fatalf("有效 Tag 结果被拒绝: %v", err)
	}
	if _, err := versioningv1.ParseRequest(versioningv1.OperationDeleteHead, marshal(t, versioningv1.DeleteHeadRequest{Stream: root.Ref.Stream, Name: "feature"})); err == nil {
		t.Fatal("删除 Head 必须使用非零 CAS revision")
	}
}

func TestCompareAndAncestryContractsRejectInconsistentResults(t *testing.T) {
	root := record(t, 1, nil, `{"a":1}`)
	child := record(t, 2, &root.Ref, `{"a":2}`)
	comparison := versioningv1.CompareVersionsResult{
		Left: root.Ref, Right: child.Ref,
		Patch:   []versioningv1.JSONPatchOperation{{Operation: "replace", Path: "/a", Value: json.RawMessage(`2`)}},
		Summary: versioningv1.ChangeSummary{Replaced: 1, Total: 1},
	}
	if _, err := versioningv1.ParseResult(versioningv1.OperationCompare, marshal(t, comparison)); err != nil {
		t.Fatalf("有效比较结果被拒绝: %v", err)
	}
	comparison.Summary.Total = 2
	if _, err := versioningv1.ParseResult(versioningv1.OperationCompare, marshal(t, comparison)); err == nil {
		t.Fatal("Patch 与统计不一致必须拒绝")
	}
	if _, err := versioningv1.ParseResult(versioningv1.OperationIsAncestor, marshal(t, versioningv1.IsAncestorResult{Distance: 0})); err == nil {
		t.Fatal("非祖先结果 distance 必须为 -1")
	}
	invalidCommon := versioningv1.FindCommonAncestorResult{Found: false, Ancestor: &root.Ref, LeftDistance: -1, RightDistance: -1}
	if _, err := versioningv1.ParseResult(versioningv1.OperationCommonAncestor, marshal(t, invalidCommon)); err == nil {
		t.Fatal("未找到共同祖先时不得携带 ancestor")
	}
}
