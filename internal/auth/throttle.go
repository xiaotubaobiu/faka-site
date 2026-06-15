package auth

import (
	"sync"
	"time"
)

// Throttle 简单内存滑动窗口限流:每个 key 每分钟最多 limit 次。
type Throttle struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

func NewThrottle(limit int) *Throttle {
	return &Throttle{limit: limit, window: time.Minute, hits: map[string][]time.Time{}, now: time.Now}
}

func (t *Throttle) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	cutoff := now.Add(-t.window)
	keep := t.hits[key][:0]
	for _, h := range t.hits[key] {
		if h.After(cutoff) {
			keep = append(keep, h)
		}
	}
	if len(keep) >= t.limit {
		t.hits[key] = keep
		return false
	}
	t.hits[key] = append(keep, now)
	return true
}
