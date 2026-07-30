package versionledger

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func TestMemoryProviderContract(t *testing.T) {
	clock := fixedClock()
	runProviderContract(t, newMemoryProvider(clock))
}

func runProviderContract(t *testing.T, provider Provider) {
	t.Helper()
	ctx := context.Background()
	scope := Scope{TenantID: "tenant-a"}
	firstRequest := putRequest("portal-main:revision:0001", nil, `{"layout":"standard"}`)
	first, err := provider.PutVersion(ctx, scope, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || first.Version.Ref.Sequence != 1 || first.Version.Ref.VersionID != firstRequest.Candidate.VersionID {
		t.Fatalf("首个版本身份无效: %+v", first)
	}
	forgedIdentity := firstRequest
	forgedIdentity.Candidate.VersionID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err := provider.PutVersion(ctx, scope, forgedIdentity); errorCode(err) != versioningv1.ErrorDigestMismatch {
		t.Fatalf("Provider 必须拒绝不符合统一算法的 versionId: %v", err)
	}
	if err := versioningv1.ValidateVersionRecord(first.Version); err != nil {
		t.Fatal(err)
	}
	reused, err := provider.PutVersion(ctx, scope, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !reused.Reused || reused.Version.Ref != first.Version.Ref || !reused.Version.CreatedAt.Equal(first.Version.CreatedAt) {
		t.Fatalf("幂等重试未返回原版本: %+v", reused)
	}
	conflicting := firstRequest
	conflicting.Candidate.Content = json.RawMessage(`{"layout":"attacker"}`)
	if _, err := provider.PutVersion(ctx, scope, conflicting); errorCode(err) != versioningv1.ErrorConflict {
		t.Fatalf("相同 idempotencyKey 的不同候选必须冲突: %v", err)
	}

	second, err := provider.PutVersion(ctx, scope, putRequest("portal-main:revision:0002", &first.Version.Ref, `{"layout":"top-navigation"}`))
	if err != nil {
		t.Fatal(err)
	}
	third, err := provider.PutVersion(ctx, scope, putRequest("portal-main:revision:0003", &second.Version.Ref, `{"layout":"compact"}`))
	if err != nil {
		t.Fatal(err)
	}
	if second.Version.Ref.Sequence != 2 || third.Version.Ref.Sequence != 3 {
		t.Fatalf("sequence 未单调分配: %d %d", second.Version.Ref.Sequence, third.Version.Ref.Sequence)
	}
	missingParent := first.Version.Ref
	missingParent.VersionID = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if _, err := provider.PutVersion(ctx, scope, putRequest("portal-main:revision:0004", &missingParent, `{}`)); errorCode(err) != versioningv1.ErrorNotFound {
		t.Fatalf("不存在的父版本必须拒绝: %v", err)
	}

	got, err := provider.GetVersion(ctx, scope, versioningv1.GetVersionRequest{Ref: second.Version.Ref})
	if err != nil || got.Version.Ref != second.Version.Ref {
		t.Fatalf("精确读取失败: %+v %v", got, err)
	}
	if _, err := provider.GetVersion(ctx, Scope{TenantID: "tenant-b"}, versioningv1.GetVersionRequest{Ref: second.Version.Ref}); errorCode(err) != versioningv1.ErrorNotFound {
		t.Fatalf("跨 tenant 读取必须隐藏为 not found: %v", err)
	}

	page, err := provider.ListHistory(ctx, scope, versioningv1.ListHistoryRequest{Stream: first.Version.Ref.Stream, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Versions) != 2 || page.Versions[0].Ref != third.Version.Ref || page.Versions[1].Ref != second.Version.Ref || page.NextCursor != first.Version.Ref.VersionID {
		t.Fatalf("历史首页无效: %+v", page)
	}
	last, err := provider.ListHistory(ctx, scope, versioningv1.ListHistoryRequest{Stream: first.Version.Ref.Stream, Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(last.Versions) != 1 || last.Versions[0].Ref != first.Version.Ref || last.NextCursor != "" {
		t.Fatalf("历史续页无效: %+v %v", last, err)
	}
	branch, err := provider.PutVersion(ctx, scope, putRequest("portal-main:branch:0001", &first.Version.Ref, `{"layout":"branch"}`))
	if err != nil {
		t.Fatal(err)
	}
	mergeRequest := putRequestWithParents("portal-main:merge:0001", []versioningv1.VersionRef{third.Version.Ref, branch.Version.Ref}, `{"layout":"merged"}`)
	merge, err := provider.PutVersion(ctx, scope, mergeRequest)
	if err != nil || len(merge.Version.Parents) != 2 || merge.Version.Parents[0] != third.Version.Ref || merge.Version.Parents[1] != branch.Version.Ref {
		t.Fatalf("双父版本写入失败: %+v %v", merge, err)
	}

	feature, err := provider.CreateHead(ctx, scope, versioningv1.CreateHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", Target: branch.Version.Ref})
	if err != nil || feature.Head.Revision != 1 || feature.Reused {
		t.Fatalf("创建 Head 失败: %+v %v", feature, err)
	}
	featureAgain, err := provider.CreateHead(ctx, scope, versioningv1.CreateHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", Target: branch.Version.Ref})
	if err != nil || !featureAgain.Reused {
		t.Fatalf("重复创建相同 Head 必须幂等: %+v %v", featureAgain, err)
	}
	if _, err := provider.CreateHead(ctx, scope, versioningv1.CreateHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", Target: merge.Version.Ref}); errorCode(err) != versioningv1.ErrorConflict {
		t.Fatalf("创建同名不同目标 Head 必须冲突: %v", err)
	}
	if _, err := provider.CreateHead(ctx, scope, versioningv1.CreateHeadRequest{Stream: first.Version.Ref.Stream, Name: "release", Target: third.Version.Ref}); err != nil {
		t.Fatal(err)
	}
	heads, err := provider.ListHeads(ctx, scope, versioningv1.ListHeadsRequest{Stream: first.Version.Ref.Stream, Limit: 1})
	if err != nil || len(heads.Heads) != 1 || heads.Heads[0].Name != "feature" || heads.NextCursor != "feature" {
		t.Fatalf("Head 分页无效: %+v %v", heads, err)
	}
	if _, err := provider.DeleteHead(ctx, scope, versioningv1.DeleteHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", ExpectedRevision: 2}); errorCode(err) != versioningv1.ErrorConflict {
		t.Fatalf("过期 Head 删除必须冲突: %v", err)
	}
	deleted, err := provider.DeleteHead(ctx, scope, versioningv1.DeleteHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", ExpectedRevision: 1})
	if err != nil || deleted.Previous.Target != branch.Version.Ref {
		t.Fatalf("Head CAS 删除失败: %+v %v", deleted, err)
	}
	recreated, err := provider.CreateHead(ctx, scope, versioningv1.CreateHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", Target: merge.Version.Ref})
	if err != nil || recreated.Head.Revision != 2 || recreated.Reused {
		t.Fatalf("Head 删除后重建必须延续 revision 以阻止 ABA: %+v %v", recreated, err)
	}
	if _, err := provider.DeleteHead(ctx, scope, versioningv1.DeleteHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", ExpectedRevision: 1}); errorCode(err) != versioningv1.ErrorConflict {
		t.Fatalf("删除前的旧 revision 不得命中新 Head: %v", err)
	}
	if _, err := provider.DeleteHead(ctx, scope, versioningv1.DeleteHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	movedAfterDelete, err := provider.MoveHead(ctx, scope, versioningv1.MoveHeadRequest{Stream: first.Version.Ref.Stream, Name: "feature", Target: branch.Version.Ref})
	if err != nil || movedAfterDelete.Head.Revision != 3 {
		t.Fatalf("Head 删除后以 move 重建必须延续 revision: %+v %v", movedAfterDelete, err)
	}

	tagRequest := versioningv1.ProviderCreateTagRequest{Stream: first.Version.Ref.Stream, Name: "release-1", Target: third.Version.Ref, ActorID: "plugin:portal-composer"}
	tag, err := provider.CreateTag(ctx, scope, tagRequest)
	if err != nil || tag.Reused || tag.Tag.Target != third.Version.Ref {
		t.Fatalf("创建不可变 Tag 失败: %+v %v", tag, err)
	}
	tagAgain, err := provider.CreateTag(ctx, scope, tagRequest)
	if err != nil || !tagAgain.Reused || !tagAgain.Tag.CreatedAt.Equal(tag.Tag.CreatedAt) {
		t.Fatalf("重复创建相同 Tag 必须幂等: %+v %v", tagAgain, err)
	}
	tagRequest.Target = merge.Version.Ref
	if _, err := provider.CreateTag(ctx, scope, tagRequest); errorCode(err) != versioningv1.ErrorConflict {
		t.Fatalf("Tag 不得改指其他版本: %v", err)
	}
	readTag, err := provider.GetTag(ctx, scope, versioningv1.GetTagRequest{Stream: first.Version.Ref.Stream, Name: "release-1"})
	if err != nil || readTag.Tag.Target != third.Version.Ref {
		t.Fatalf("读取 Tag 失败: %+v %v", readTag, err)
	}
	tags, err := provider.ListTags(ctx, scope, versioningv1.ListTagsRequest{Stream: first.Version.Ref.Stream, Limit: 10})
	if err != nil || len(tags.Tags) != 1 || tags.Tags[0].Name != "release-1" {
		t.Fatalf("Tag 列表无效: %+v %v", tags, err)
	}

	head, err := provider.MoveHead(ctx, scope, versioningv1.MoveHeadRequest{Stream: first.Version.Ref.Stream, Name: "published", Target: first.Version.Ref})
	if err != nil || head.Head.Revision != 1 {
		t.Fatalf("首次 Head CAS 失败: %+v %v", head, err)
	}
	if _, err := provider.MoveHead(ctx, scope, versioningv1.MoveHeadRequest{Stream: first.Version.Ref.Stream, Name: "published", Target: second.Version.Ref}); errorCode(err) != versioningv1.ErrorConflict {
		t.Fatalf("过期 Head revision 必须冲突: %v", err)
	}
	readHead, err := provider.GetHead(ctx, scope, versioningv1.GetHeadRequest{Stream: first.Version.Ref.Stream, Name: "published"})
	if err != nil || readHead.Head.Target != first.Version.Ref {
		t.Fatalf("读取 Head 失败: %+v %v", readHead, err)
	}

	requests := []versioningv1.MoveHeadRequest{
		{Stream: first.Version.Ref.Stream, Name: "published", Target: second.Version.Ref, ExpectedRevision: 1},
		{Stream: first.Version.Ref.Stream, Name: "published", Target: third.Version.Ref, ExpectedRevision: 1},
	}
	var wait sync.WaitGroup
	wait.Add(len(requests))
	codes := make(chan string, len(requests))
	for _, request := range requests {
		go func() {
			defer wait.Done()
			_, moveErr := provider.MoveHead(ctx, scope, request)
			codes <- errorCode(moveErr)
		}()
	}
	wait.Wait()
	close(codes)
	successes, conflicts := 0, 0
	for code := range codes {
		switch code {
		case "":
			successes++
		case versioningv1.ErrorConflict:
			conflicts++
		default:
			t.Fatalf("并发 Head CAS 返回意外错误码: %s", code)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("Head CAS 必须只有一个赢家: success=%d conflict=%d", successes, conflicts)
	}
}

func putRequest(key string, parent *versioningv1.VersionRef, content string) versioningv1.ProviderPutVersionRequest {
	return putRequestWithParents(key, providerRefs(parent), content)
}

func putRequestWithParents(key string, parents []versioningv1.VersionRef, content string) versioningv1.ProviderPutVersionRequest {
	stream := versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "portal-main"}
	versionID, err := versioningv1.DeriveVersionID("tenant-a", stream, key)
	if err != nil {
		panic(err)
	}
	return versioningv1.ProviderPutVersionRequest{
		IdempotencyKey: key,
		Candidate: versioningv1.ProviderVersionCandidate{
			VersionID: versionID, Stream: stream,
			Parents: cloneRefs(parents), Content: json.RawMessage(content), ActorID: "plugin:portal-composer",
		},
	}
}

func providerRefs(parent *versioningv1.VersionRef) []versioningv1.VersionRef {
	if parent == nil {
		return nil
	}
	return []versioningv1.VersionRef{*parent}
}

func fixedClock() func() time.Time {
	var mu sync.Mutex
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		current := now
		now = now.Add(time.Second)
		return current
	}
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Code
	}
	return "unknown"
}
