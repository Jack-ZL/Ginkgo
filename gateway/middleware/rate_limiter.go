package middleware

import (
	"ginkgo/config"
	"net/http"
	"sync"
	"time"
)

// RateLimiter 限制请求频率
type RateLimiter struct {
	requests int            // 允许的最大请求数
	interval time.Duration  // 时间窗口
	counts   map[string]int // 记录每个客户端的请求次数
	mu       sync.Mutex     // 保护counts的并发访问
}

// NewRateLimiter 初始化限流器
func NewRateLimiter(requests int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: requests,
		interval: interval,
		counts:   make(map[string]int),
	}
}

// Middleware 限流逻辑
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr // 获取客户端IP
		rl.mu.Lock()
		defer rl.mu.Unlock()

		// 检查请求次数是否超过限制
		if rl.counts[ip] >= rl.requests {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		rl.counts[ip]++

		// 重置计数
		time.AfterFunc(rl.interval, func() {
			rl.mu.Lock()
			defer rl.mu.Unlock()
			rl.counts[ip]--
		})

		next.ServeHTTP(w, r)
	})
}

// RateLimiterMiddleware 创建一个限流中间件实例
func RateLimiterMiddleware(next http.Handler) http.Handler {
	// 从配置中获取限流规则
	requests := config.GetRateLimit()
	interval := time.Minute
	rl := NewRateLimiter(requests, interval)
	return rl.Middleware(next)
}
