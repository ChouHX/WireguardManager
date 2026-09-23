package middleware

import (
	"testing"
	"time"
)

// 限速器直接决定登录/注册接口能否被爆破，这里把窗口计数、重置与容量上限钉住。
func TestIPRateLimiterAllow(t *testing.T) {
	t.Run("窗口内放行到限额后拒绝", func(t *testing.T) {
		l := newIPRateLimiter(RateLimitConfig{Limit: 3, Window: time.Minute})

		for i := 0; i < 3; i++ {
			if ok, _ := l.allow("1.2.3.4"); !ok {
				t.Fatalf("第 %d 次请求应当放行", i+1)
			}
		}

		ok, retryAfter := l.allow("1.2.3.4")
		if ok {
			t.Fatal("超出限额的请求应当被拒绝")
		}
		if retryAfter <= 0 || retryAfter > time.Minute {
			t.Fatalf("Retry-After 不合理: %v", retryAfter)
		}
	})

	t.Run("不同来源互不影响", func(t *testing.T) {
		l := newIPRateLimiter(RateLimitConfig{Limit: 1, Window: time.Minute})

		if ok, _ := l.allow("1.1.1.1"); !ok {
			t.Fatal("来源 A 的首次请求应当放行")
		}
		if ok, _ := l.allow("1.1.1.1"); ok {
			t.Fatal("来源 A 的第二次请求应当被拒绝")
		}
		if ok, _ := l.allow("2.2.2.2"); !ok {
			t.Fatal("来源 B 不应受来源 A 的计数影响")
		}
	})

	t.Run("窗口过期后重新计数", func(t *testing.T) {
		l := newIPRateLimiter(RateLimitConfig{Limit: 1, Window: 10 * time.Millisecond})

		if ok, _ := l.allow("3.3.3.3"); !ok {
			t.Fatal("首次请求应当放行")
		}
		if ok, _ := l.allow("3.3.3.3"); ok {
			t.Fatal("窗口内第二次请求应当被拒绝")
		}

		time.Sleep(20 * time.Millisecond)

		if ok, _ := l.allow("3.3.3.3"); !ok {
			t.Fatal("窗口过期后应当重新放行")
		}
	})

	t.Run("限额为 0 表示不限速", func(t *testing.T) {
		l := newIPRateLimiter(RateLimitConfig{Limit: 0, Window: time.Minute})

		for i := 0; i < 100; i++ {
			if ok, _ := l.allow("4.4.4.4"); !ok {
				t.Fatal("限额为 0 时不应拦截任何请求")
			}
		}
	})
}

// 限速表必须有界：否则攻击者用大量不同来源打过来即可把内存撑爆。
func TestIPRateLimiterBoundsEntries(t *testing.T) {
	const maxKeys = 16
	l := newIPRateLimiter(RateLimitConfig{Limit: 5, Window: time.Minute, MaxKeys: maxKeys})

	for i := 0; i < maxKeys*20; i++ {
		l.allow(string(rune('a'+i%26)) + "-" + time.Duration(i).String())
	}

	l.mu.Lock()
	size := len(l.entries)
	l.mu.Unlock()

	if size > maxKeys {
		t.Fatalf("限速表条目数 %d 超过上限 %d", size, maxKeys)
	}
}

// 容量压力下仍应正常放行新来源，而不是把请求全部拒掉。
func TestIPRateLimiterKeepsServingUnderPressure(t *testing.T) {
	l := newIPRateLimiter(RateLimitConfig{Limit: 2, Window: time.Minute, MaxKeys: 8})

	for i := 0; i < 200; i++ {
		if ok, _ := l.allow("new-" + time.Duration(i).String()); !ok {
			t.Fatalf("第 %d 个新来源应当被放行", i)
		}
	}
}

// 容量已满时应优先淘汰最早过期的条目，而不是误删仍在窗口内的活跃来源。
func TestIPRateLimiterEvictsSoonestExpiringFirst(t *testing.T) {
	l := newIPRateLimiter(RateLimitConfig{Limit: 5, Window: time.Minute, MaxKeys: 3})

	// 先建立三个不同剩余时长的来源
	if ok, _ := l.allow("oldest"); !ok {
		t.Fatal("oldest 应当放行")
	}
	time.Sleep(2 * time.Millisecond)
	if ok, _ := l.allow("middle"); !ok {
		t.Fatal("middle 应当放行")
	}
	time.Sleep(2 * time.Millisecond)
	if ok, _ := l.allow("newest"); !ok {
		t.Fatal("newest 应当放行")
	}

	// 触发容量淘汰
	if ok, _ := l.allow("trigger"); !ok {
		t.Fatal("trigger 应当放行")
	}

	l.mu.Lock()
	_, hasOldest := l.entries["oldest"]
	_, hasNewest := l.entries["newest"]
	l.mu.Unlock()

	if hasOldest {
		t.Fatal("最早过期的条目应被优先淘汰")
	}
	if !hasNewest {
		t.Fatal("最新建立的条目不应被淘汰")
	}
}
