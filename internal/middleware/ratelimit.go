package middleware

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"cloud-platform/internal/response"

	"github.com/gin-gonic/gin"
)

// RateLimitConfig 描述一个固定窗口限速。
type RateLimitConfig struct {
	// Limit 一个窗口内允许的请求数；<= 0 表示不限速。
	Limit int
	// Window 窗口长度。
	Window time.Duration
	// MaxKeys 同时跟踪的来源数量上限。
	//
	// 必须有界：限速表以客户端地址为键，若任由其增长，攻击者用大量不同来源
	// 打过来就能把内存撑爆，限速器本身反而成了放大器。
	MaxKeys int
}

// rateLimitEntry 单个来源的计数状态。
type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

// ipRateLimiter 按来源地址计数的固定窗口限速器。
//
// 选用固定窗口是因为它只需一个计数器和一次时间比较，开销足够低，可以放在
// 每个敏感请求的路径上；代价是窗口边界处最多放过 2 倍限额，对"防爆破"这个
// 目的完全够用，不必引入滑动窗口的额外状态。
type ipRateLimiter struct {
	cfg RateLimitConfig

	mu      sync.Mutex
	entries map[string]*rateLimitEntry
}

func newIPRateLimiter(cfg RateLimitConfig) *ipRateLimiter {
	if cfg.MaxKeys <= 0 {
		cfg.MaxKeys = 4096
	}
	return &ipRateLimiter{
		cfg:     cfg,
		entries: make(map[string]*rateLimitEntry, 64),
	}
}

// allow 记录一次访问并返回是否放行，以及被拒时需要等待的时长。
func (l *ipRateLimiter) allow(key string) (bool, time.Duration) {
	if l.cfg.Limit <= 0 {
		return true, 0
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok || now.After(entry.resetAt) {
		if len(l.entries) >= l.cfg.MaxKeys {
			l.shrinkLocked(now)
		}
		l.entries[key] = &rateLimitEntry{count: 1, resetAt: now.Add(l.cfg.Window)}
		return true, 0
	}

	if entry.count >= l.cfg.Limit {
		return false, time.Until(entry.resetAt)
	}

	entry.count++
	return true, 0
}

// shrinkLocked 把限速表压回容量之内。调用方必须持有 l.mu。
//
// 先按过期时间排序再整体截断，单次 O(n log n) 且只做一轮；相比"每次都全表扫一遍
// 找最旧的一条"，它在最坏情况下的开销可控，而这种情况只在短时间内涌入大量
// 不同来源时才会出现。
func (l *ipRateLimiter) shrinkLocked(now time.Time) {
	type keyedExpiry struct {
		key     string
		resetAt time.Time
	}

	alive := make([]keyedExpiry, 0, len(l.entries))
	for key, entry := range l.entries {
		if now.After(entry.resetAt) {
			delete(l.entries, key)
			continue
		}
		alive = append(alive, keyedExpiry{key: key, resetAt: entry.resetAt})
	}

	// 清理过期的条目后若已回到容量之内，无需再淘汰仍在生效的条目
	if len(l.entries) < l.cfg.MaxKeys {
		return
	}

	sort.Slice(alive, func(i, j int) bool { return alive[i].resetAt.Before(alive[j].resetAt) })

	// 腾出一格给即将写入的新来源
	target := l.cfg.MaxKeys - 1
	for i := 0; i < len(alive) && len(l.entries) > target; i++ {
		delete(l.entries, alive[i].key)
	}
}

// RateLimitByIP 返回一个按来源地址限速的中间件。
//
// 注意它依赖 c.ClientIP()：若未调用 Engine.SetTrustedProxies 收窄可信代理，
// gin 默认信任所有来源，客户端可直接用 X-Forwarded-For 伪造地址绕过限速。
// 调用方必须先设置可信代理（见 main.go 的 setupTrustedProxies）。
func RateLimitByIP(cfg RateLimitConfig) gin.HandlerFunc {
	limiter := newIPRateLimiter(cfg)

	return func(c *gin.Context) {
		allowed, retryAfter := limiter.allow(c.ClientIP())
		if !allowed {
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			response.TooManyRequests(c, "Too many requests, please try again later")
			c.Abort()
			return
		}
		c.Next()
	}
}
