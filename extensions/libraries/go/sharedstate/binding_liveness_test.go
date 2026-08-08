package sharedstate

import (
	"context"
	"testing"
)

// flakyStore answers or reports unavailability on demand, standing in for a
// remote provider whose instances come and go.
type flakyStore struct{ err error }

func (s *flakyStore) Get(context.Context, Scope, string) (Entry, error) {
	return Entry{}, s.err
}

func (s *flakyStore) Create(context.Context, Scope, string, []byte) (Entry, error) {
	return Entry{}, s.err
}

func (s *flakyStore) Update(context.Context, Scope, string, []byte, uint64) (Entry, error) {
	return Entry{}, s.err
}

func (s *flakyStore) Delete(context.Context, Scope, string, uint64) error { return s.err }

func (s *flakyStore) List(context.Context, Scope, string, int, string) (Page, error) {
	return Page{}, s.err
}

// A provider that was bound and then became unreachable must stop reporting
// itself as usable. Snapshot deliberately keeps reporting the binding, so the
// two signals have to disagree here.
func TestLiveFallsBackWhenBoundProviderStopsAnswering(t *testing.T) {
	binding := NewBindingStore()
	store := &flakyStore{}
	if err := binding.Bind(1, "identity", store); err != nil {
		t.Fatal(err)
	}
	if !binding.Live() {
		t.Fatal("刚绑定的 Provider 必须是 live")
	}

	store.err = ErrUnavailable
	if _, err := binding.Get(context.Background(), Scope{}, "probe"); err == nil {
		t.Fatal("期望调用失败")
	}
	if binding.Live() {
		t.Fatal("Provider 不可达后 Live 必须回落，否则激活门禁会持续放行")
	}
	if _, _, ready := binding.Snapshot(); !ready {
		t.Fatal("Snapshot 仍应报告曾经绑定过，两个信号语义不同")
	}
}

// Application errors prove the provider answered, so they must not be read as
// an outage.
func TestLiveSurvivesApplicationErrors(t *testing.T) {
	for name, storeErr := range map[string]error{
		"not_found": ErrNotFound,
		"conflict":  ErrConflict,
		"invalid":   ErrInvalid,
	} {
		t.Run(name, func(t *testing.T) {
			binding := NewBindingStore()
			if err := binding.Bind(1, "identity", &flakyStore{err: storeErr}); err != nil {
				t.Fatal(err)
			}
			if _, err := binding.Get(context.Background(), Scope{}, "probe"); err == nil {
				t.Fatal("期望调用返回应用错误")
			}
			if !binding.Live() {
				t.Fatal("应用错误证明 Provider 已应答，不得判为不可用")
			}
		})
	}
}

// Recovery must not require a rebind: once the provider answers again the gate
// has to reopen on its own.
func TestLiveRecoversAfterProviderAnswersAgain(t *testing.T) {
	binding := NewBindingStore()
	store := &flakyStore{err: ErrUnavailable}
	if err := binding.Bind(1, "identity", store); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Get(context.Background(), Scope{}, "probe"); err == nil {
		t.Fatal("期望调用失败")
	}
	if binding.Live() {
		t.Fatal("Provider 不可达后 Live 必须回落")
	}

	store.err = nil
	if _, err := binding.Get(context.Background(), Scope{}, "probe"); err != nil {
		t.Fatal(err)
	}
	if !binding.Live() {
		t.Fatal("Provider 恢复应答后 Live 必须自行恢复，不应等待重新绑定")
	}
}

// Liveness must not become a second route back to the unconfigured state: a
// committed profile that never bound stays fail-closed on unavailable.
func TestLiveDoesNotWeakenUnconfiguredBoundary(t *testing.T) {
	binding := NewBindingStore()
	if binding.Live() {
		t.Fatal("从未绑定时不得报 live")
	}
	binding.RequireProvider()
	if binding.Live() {
		t.Fatal("已声明必需但尚未绑定时不得报 live")
	}
	if _, err := binding.Get(context.Background(), Scope{}, "probe"); !IsUnavailable(err) {
		t.Fatalf("已声明必需时必须 fail-closed 为 unavailable: %v", err)
	}
}
