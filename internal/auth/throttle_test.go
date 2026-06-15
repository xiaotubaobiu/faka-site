package auth

import (
	"testing"
	"time"
)

func TestThrottle(t *testing.T) {
	base := time.Unix(1000, 0)
	clock := base
	th := &Throttle{limit: 3, window: time.Minute, hits: map[string][]time.Time{}, now: func() time.Time { return clock }}
	if !th.Allow("ip") || !th.Allow("ip") || !th.Allow("ip") {
		t.Fatal("first 3 should be allowed")
	}
	if th.Allow("ip") {
		t.Fatal("4th should be blocked")
	}
	clock = clock.Add(2 * time.Minute)
	if !th.Allow("ip") {
		t.Fatal("after window should be allowed again")
	}
}
