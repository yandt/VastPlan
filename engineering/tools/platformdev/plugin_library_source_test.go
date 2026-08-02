package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitForPluginLibraryRepositoryRecoversStartupRace(t *testing.T) {
	attempts := 0
	err := waitForPluginLibraryRepository(context.Background(), time.Millisecond, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("repository socket 尚未创建")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("仓库就绪后应自动继续: attempts=%d err=%v", attempts, err)
	}
}

func TestWaitForPluginLibraryRepositoryStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForPluginLibraryRepository(ctx, time.Hour, func(context.Context) error {
		return errors.New("repository unavailable")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("停止时必须返回 context canceled: %v", err)
	}
}
