package worker

import (
	"testing"
	"time"
)

func TestRetryDelay(t *testing.T) {
	base, max := time.Second, time.Minute
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for attempt, w := range want {
		if d := retryDelay(attempt, base, max); d != w {
			t.Errorf("attempt %d: %s want %s", attempt, d, w)
		}
	}
	if d := retryDelay(50, base, max); d != max {
		t.Errorf("capped: %s", d)
	}
}
