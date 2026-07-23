package platformcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func TestCandidateLifecycleLocksOnlyTargetBindingAndFinalizesIdempotently(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := candidateTestCatalog()
	store, err := NewWritableStore(buckets.BackendPlatformCatalogs, buckets.BackendPlatformCatalogs, seed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	request := candidateTestRequest(seed, "pcfg_"+strings.Repeat("1", 32), strings.Repeat("a", 64))
	prepared, err := store.Prepare(ctx, request)
	if err != nil || prepared.Status != CandidatePrepared {
		t.Fatalf("准备候选失败: candidate=%+v err=%v", prepared, err)
	}
	retried, err := store.Prepare(ctx, request)
	if err != nil || retried.CreatedAt != prepared.CreatedAt {
		t.Fatalf("相同候选重试必须幂等: candidate=%+v err=%v", retried, err)
	}
	tamperedRetry := request
	tamperedRetry.NextCatalogRevision++
	if _, err := store.Prepare(ctx, tamperedRetry); !errors.Is(err, ErrCatalogConflict) {
		t.Fatalf("相同候选身份不得掩盖不同请求内容: %v", err)
	}
	if _, err := store.SnapshotForBinding(ctx, "tenant-a", "services-a"); !errors.Is(err, ErrBindingLocked) {
		t.Fatalf("目标 binding 未被锁定: %v", err)
	}
	if other, err := store.SnapshotForBinding(ctx, "tenant-b", "services-b"); err != nil || other.Digest() != seed.Digest() {
		t.Fatalf("无关 binding 不应被阻塞: catalog=%+v err=%v", other, err)
	}
	conflicting := candidateTestRequest(seed, "pcfg_"+strings.Repeat("2", 32), strings.Repeat("b", 64))
	if _, err := store.Prepare(ctx, conflicting); !errors.Is(err, ErrCandidateLocked) {
		t.Fatalf("单 Catalog 不得同时存在两个活动候选: %v", err)
	}
	activated, err := store.Activate(ctx, request.CandidateID, request.RequestDigest)
	if err != nil || activated.Status != CandidateActivated {
		t.Fatalf("激活候选失败: candidate=%+v err=%v", activated, err)
	}
	if _, err := store.SnapshotForCandidate(ctx, request.CandidateID, request.RequestDigest); err != nil {
		t.Fatalf("精确候选未能取得活动快照: %v", err)
	}
	active, err := store.Snapshot(ctx)
	if err != nil || active.Revision != seed.Revision+1 || !catalogBindsProfile(active, "tenant-a", "services-a", request.NextProfile) {
		t.Fatalf("活动 Catalog 未切换到候选 Profile: catalog=%+v err=%v", active, err)
	}
	finalized, err := store.Finalize(ctx, request.CandidateID, request.RequestDigest)
	if err != nil || finalized.Status != CandidateFinalized {
		t.Fatalf("完成候选失败: candidate=%+v err=%v", finalized, err)
	}
	if _, err := store.Finalize(ctx, request.CandidateID, request.RequestDigest); err != nil {
		t.Fatalf("完成重试必须幂等: %v", err)
	}
	if _, err := store.SnapshotForBinding(ctx, "tenant-a", "services-a"); err != nil {
		t.Fatalf("终态候选不得继续锁定 binding: %v", err)
	}
}

func TestCandidateAbortAndRollbackAreMonotonic(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := candidateTestCatalog()
	store, _ := NewWritableStore(buckets.BackendPlatformCatalogs, buckets.BackendPlatformCatalogs, seed)
	_, _ = store.Seed(ctx)

	abortedRequest := candidateTestRequest(seed, "pcfg_"+strings.Repeat("3", 32), strings.Repeat("c", 64))
	aborted, err := store.Prepare(ctx, abortedRequest)
	if err != nil {
		t.Fatal(err)
	}
	aborted, err = store.Abort(ctx, aborted.CandidateID, aborted.RequestDigest)
	if err != nil || aborted.Status != CandidateAborted {
		t.Fatalf("放弃 Prepared 候选失败: candidate=%+v err=%v", aborted, err)
	}
	if _, err := store.Activate(ctx, aborted.CandidateID, aborted.RequestDigest); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("已放弃候选不得重新激活: %v", err)
	}

	rollbackRequest := candidateTestRequest(seed, "pcfg_"+strings.Repeat("4", 32), strings.Repeat("d", 64))
	if _, err := store.Prepare(ctx, rollbackRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(ctx, rollbackRequest.CandidateID, rollbackRequest.RequestDigest); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := store.Rollback(ctx, rollbackRequest.CandidateID, rollbackRequest.RequestDigest)
	if err != nil || rolledBack.Status != CandidateRolledBack || rolledBack.RollbackCatalogDigest == "" {
		t.Fatalf("回滚候选失败: candidate=%+v err=%v", rolledBack, err)
	}
	rollbackCatalog, err := store.Snapshot(ctx)
	if err != nil || rollbackCatalog.Revision != seed.Revision+2 || rollbackCatalog.Digest() != rolledBack.RollbackCatalogDigest ||
		!catalogBindsRef(rollbackCatalog, "tenant-a", "services-a", rollbackRequest.ExpectedProfile) {
		t.Fatalf("Catalog 回滚不是新单调修订: catalog=%+v err=%v", rollbackCatalog, err)
	}
	if repeated, err := store.Rollback(ctx, rollbackRequest.CandidateID, rollbackRequest.RequestDigest); err != nil || repeated.RollbackCatalogDigest != rolledBack.RollbackCatalogDigest {
		t.Fatalf("回滚重试必须幂等: candidate=%+v err=%v", repeated, err)
	}
}

func TestConcurrentCandidatePrepareHasSingleCASWinner(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := candidateTestCatalog()
	store, _ := NewWritableStore(buckets.BackendPlatformCatalogs, buckets.BackendPlatformCatalogs, seed)
	_, _ = store.Seed(ctx)
	requests := []PrepareRequest{
		candidateTestRequest(seed, "pcfg_"+strings.Repeat("5", 32), strings.Repeat("e", 64)),
		candidateTestRequest(seed, "pcfg_"+strings.Repeat("6", 32), strings.Repeat("f", 64)),
	}
	start := make(chan struct{})
	errorsByRequest := make([]error, len(requests))
	var wait sync.WaitGroup
	for index := range requests {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByRequest[index] = store.Prepare(ctx, requests[index])
		}(index)
	}
	close(start)
	wait.Wait()
	winners, locked := 0, 0
	for _, err := range errorsByRequest {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrCandidateLocked):
			locked++
		default:
			t.Fatalf("并发 Prepare 返回非预期错误: %v", err)
		}
	}
	if winners != 1 || locked != 1 {
		t.Fatalf("并发 Prepare 必须恰有一个 CAS 获胜: winners=%d locked=%d", winners, locked)
	}
}

func TestCandidateStateTamperingFailsClosed(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := candidateTestCatalog()
	store, _ := NewWritableStore(buckets.BackendPlatformCatalogs, buckets.BackendPlatformCatalogs, seed)
	_, _ = store.Seed(ctx)
	request := candidateTestRequest(seed, "pcfg_"+strings.Repeat("7", 32), strings.Repeat("7", 64))
	if _, err := store.Prepare(ctx, request); err != nil {
		t.Fatal(err)
	}
	entry, _ := buckets.BackendPlatformCatalogs.Get(ctx, store.key)
	var state persistedSnapshot
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		t.Fatal(err)
	}
	state.Candidate.NextProfile.Revision++
	raw, _ := json.Marshal(state)
	if _, err := buckets.BackendPlatformCatalogs.Update(ctx, store.key, raw, entry.Revision()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(ctx); err == nil {
		t.Fatal("候选 delta 被篡改时必须 fail-closed")
	}
}

func TestReadOnlyCatalogStoreCannotMutateCandidateState(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := candidateTestCatalog()
	readOnly, _ := NewStore(buckets.BackendPlatformCatalogs, seed)
	if _, err := readOnly.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	request := candidateTestRequest(seed, "pcfg_"+strings.Repeat("8", 32), strings.Repeat("8", 64))
	if _, err := readOnly.Prepare(ctx, request); err == nil {
		t.Fatal("未注入 catalog-publisher KV 的 Store 不得写候选")
	}
}

func candidateTestCatalog() backendcompositionv1.BackendPlatformCatalog {
	first := testCatalog(1)
	secondProfile := first.Profiles[0]
	secondProfile.ID = "backend-secondary"
	secondRef := compositioncommonv1.Ref{ID: secondProfile.ID, Revision: secondProfile.Revision, Digest: secondProfile.Digest()}
	first.Profiles = append(first.Profiles, secondProfile)
	first.Bindings[0].TenantID, first.Bindings[0].DeploymentName = "tenant-a", "services-a"
	first.Bindings = append(first.Bindings, backendcompositionv1.BackendPlatformBinding{TenantID: "tenant-b", DeploymentName: "services-b", PlatformProfile: secondRef})
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(first)
	if err != nil {
		panic(err)
	}
	return validated
}

func candidateTestRequest(active backendcompositionv1.BackendPlatformCatalog, candidateID, requestDigest string) PrepareRequest {
	profile, previous, err := active.Resolve("tenant-a", "services-a")
	if err != nil {
		panic(err)
	}
	profile.Revision++
	next := cloneCatalog(active)
	next.Revision++
	next.Profiles = append(next.Profiles, profile)
	nextRef := compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}
	if !replaceBinding(&next, "tenant-a", "services-a", nextRef) {
		panic("missing candidate binding")
	}
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(next)
	if err != nil {
		panic(err)
	}
	return PrepareRequest{
		CandidateID: candidateID, RequestDigest: requestDigest, TenantID: "tenant-a", DeploymentName: "services-a",
		ExpectedCatalogDigest: active.Digest(), ExpectedProfile: previous, NextProfile: profile,
		NextCatalogRevision: validated.Revision, NextCatalogDigest: validated.Digest(),
	}
}
