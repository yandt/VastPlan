package nodeagent

import (
	"testing"
	"time"
)

func TestBackoffIsExponentialAndCapped(t *testing.T) {
	minimum, maximum := 100*time.Millisecond, 750*time.Millisecond
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 750 * time.Millisecond, 750 * time.Millisecond}
	for i, expected := range want {
		if got := backoff(minimum, maximum, i+1); got != expected {
			t.Fatalf("attempt %d backoff=%v want=%v", i+1, got, expected)
		}
	}
}
