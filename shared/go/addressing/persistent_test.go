package addressing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
)

func TestPersistentEventRedeliveryAndOrder(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	router := newTestRouter(t, server, buckets.Capabilities, "persistent-consumer")

	var mu sync.Mutex
	seen := make([]string, 0, 3)
	firstAttempt := true
	done := make(chan struct{})
	subscription, err := router.SubscribePersistent(context.Background(), PersistentSubscriptionOptions{
		Durable: "redelivery-order", EventType: "task.completed",
		AckWait: time.Second, RetryDelay: 20 * time.Millisecond,
	}, func(_ context.Context, _ *contractv1.CallContext, event *contractv1.CallEvent) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, event.Id)
		if event.Id == "event-1" && firstAttempt {
			firstAttempt = false
			return errors.New("模拟瞬时失败")
		}
		if len(seen) == 3 {
			close(done)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	for _, id := range []string{"event-1", "event-2"} {
		if err := router.PublishPersistent(context.Background(), nil, &contractv1.CallEvent{
			Id: id, Type: "task.completed", Source: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("等待持久事件重投超时")
	}
	mu.Lock()
	defer mu.Unlock()
	if got := seen; len(got) != 3 || got[0] != "event-1" || got[1] != "event-1" || got[2] != "event-2" {
		t.Fatalf("至少一次和严格顺序不符合预期: %v", got)
	}
}

func TestPersistentEventDurableResumeAndDeduplication(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	router := newTestRouter(t, server, buckets.Capabilities, "persistent-resume")

	first, err := router.SubscribePersistent(context.Background(), PersistentSubscriptionOptions{
		Durable: "resume-durable", EventType: "workflow.progress",
	}, func(context.Context, *contractv1.CallContext, *contractv1.CallEvent) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	event := &contractv1.CallEvent{Id: "dedupe-event", Type: "workflow.progress", Source: "test"}
	if err := router.PublishPersistent(context.Background(), nil, event); err != nil {
		t.Fatal(err)
	}
	if err := router.PublishPersistent(context.Background(), nil, event); err != nil {
		t.Fatal(err)
	}

	received := make(chan string, 2)
	second, err := router.SubscribePersistent(context.Background(), PersistentSubscriptionOptions{
		Durable: "resume-durable", EventType: "workflow.progress",
	}, func(_ context.Context, _ *contractv1.CallContext, event *contractv1.CallEvent) error {
		received <- event.Id
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	select {
	case id := <-received:
		if id != event.Id {
			t.Fatalf("恢复后事件 id=%q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("durable 恢复后没有收到离线期间事件")
	}
	select {
	case duplicate := <-received:
		t.Fatalf("相同事件 ID 应被去重，却再次收到 %q", duplicate)
	case <-time.After(150 * time.Millisecond):
	}

	info, err := buckets.Events.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("去重后 stream 应只有一条消息，实际 %d", info.State.Msgs)
	}
}
