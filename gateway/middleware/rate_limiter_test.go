package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

// TestClientIP 验证客户端 IP 提取的优先级与端口剥离（历史 bug 回归测试）。
func TestClientIP(t *testing.T) {
	cases := []struct{ name, xff, xri, remote, want string }{
		{"XFF 单个", "1.2.3.4", "", "127.0.0.1:5555", "1.2.3.4"},
		{"XFF 多个取第一个", "1.2.3.4, 5.6.7.8", "", "127.0.0.1:5555", "1.2.3.4"},
		{"X-Real-IP", "", "9.9.9.9", "127.0.0.1:5555", "9.9.9.9"},
		{"RemoteAddr 剥离端口", "", "", "127.0.0.1:5555", "127.0.0.1"},
		{"XFF 优先于 X-Real-IP", "1.2.3.4", "9.9.9.9", "127.0.0.1:5555", "1.2.3.4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if c.xri != "" {
				r.Header.Set("X-Real-IP", c.xri)
			}
			if got := clientIP(r); got != c.want {
				t.Fatalf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
}

// TestMemoryRateLimiter 验证单机限流：阈值内放行、超限 429、不同 IP 配额独立。
func TestMemoryRateLimiter(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	h := rl.Middleware(http.HandlerFunc(okHandler))
	do := func(xff string) *httptest.ResponseRecorder {
		return doReq(h, "GET", "/", map[string]string{"X-Forwarded-For": xff})
	}

	for i := 0; i < 3; i++ {
		if rec := do("7.7.7.7"); rec.Code != http.StatusOK {
			t.Fatalf("req %d: code = %d, want 200", i, rec.Code)
		}
	}
	rec := do("7.7.7.7") // 第 4 个请求超限
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit: code = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 response should carry Retry-After header")
	}
	if rec := do("8.8.8.8"); rec.Code != http.StatusOK { // 其他 IP 不受影响
		t.Fatalf("different ip: code = %d, want 200", rec.Code)
	}
}

// TestMemoryRateLimiterWindowRelease 验证窗口到期后配额释放，且计数条目被清理（内存泄漏回归测试）。
func TestMemoryRateLimiterWindowRelease(t *testing.T) {
	rl := NewRateLimiter(1, 30*time.Millisecond) // 短窗口便于测试
	h := rl.Middleware(http.HandlerFunc(okHandler))
	do := func() int {
		// httptest.NewRequest 默认 RemoteAddr 固定 → 同一客户端 IP
		return doReq(h, "GET", "/", nil).Code
	}

	if code := do(); code != http.StatusOK {
		t.Fatalf("first: code = %d, want 200", code)
	}
	if code := do(); code != http.StatusTooManyRequests {
		t.Fatalf("second: code = %d, want 429", code)
	}
	time.Sleep(60 * time.Millisecond) // 等待窗口释放
	if code := do(); code != http.StatusOK {
		t.Fatalf("after window: code = %d, want 200 (quota should be released)", code)
	}

	// 等最后一个 AfterFunc 执行完毕后，counts 应被清空
	time.Sleep(60 * time.Millisecond)
	rl.mu.Lock()
	n := len(rl.counts)
	rl.mu.Unlock()
	if n != 0 {
		t.Fatalf("counts should be cleaned up, got %d entries", n)
	}
}

// setupRedis 启动一个内存 Redis（miniredis）并让中间件包指向它。
// 返回 miniredis 实例，测试中可 Close 模拟 Redis 宕机。
func setupRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	old := rdb
	rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
		rdb = old
	})
	return mr
}

// TestRedisRateLimiter 验证分布式限流：计数写入 Redis、超限 429、不同 IP 独立。
func TestRedisRateLimiter(t *testing.T) {
	mr := setupRedis(t)
	rrl := &RedisRateLimiter{requests: 3, window: time.Minute, fallback: NewRateLimiter(3, time.Minute)}
	h := rrl.Middleware(http.HandlerFunc(okHandler))
	do := func(xff string) *httptest.ResponseRecorder {
		return doReq(h, "GET", "/", map[string]string{"X-Forwarded-For": xff})
	}

	for i := 0; i < 3; i++ {
		if rec := do("7.7.7.7"); rec.Code != http.StatusOK {
			t.Fatalf("req %d: code = %d, want 200", i, rec.Code)
		}
	}
	rec := do("7.7.7.7")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit: code = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 should carry Retry-After")
	}
	// 计数确实存在 Redis 中（分布式语义：其他网关实例可见）
	if !mr.Exists(rateLimitKeyPrefix + "7.7.7.7") {
		t.Fatal("counter key should exist in redis")
	}
	// 不同 IP 配额独立
	if rec := do("8.8.8.8"); rec.Code != http.StatusOK {
		t.Fatalf("different ip: code = %d, want 200", rec.Code)
	}
}

// TestRedisRateLimiterFallback 验证 Redis 宕机后自动降级为单机限流（仍有限流能力，不是直接放行）。
func TestRedisRateLimiterFallback(t *testing.T) {
	mr := setupRedis(t)
	rrl := &RedisRateLimiter{requests: 2, window: time.Minute, fallback: NewRateLimiter(2, time.Minute)}
	h := rrl.Middleware(http.HandlerFunc(okHandler))
	do := func() int {
		return doReq(h, "GET", "/", map[string]string{"X-Forwarded-For": "9.9.9.9"}).Code
	}

	// Redis 正常：前 2 个放行，第 3 个 429
	if do() != http.StatusOK || do() != http.StatusOK {
		t.Fatal("first two requests should pass")
	}
	if do() != http.StatusTooManyRequests {
		t.Fatal("third request should be limited by redis")
	}

	// 模拟 Redis 宕机 → 降级为单机限流器接管
	mr.Close()
	// 降级后是全新的本地计数：还能放行 2 个，第 3 个仍被限流
	if do() != http.StatusOK || do() != http.StatusOK {
		t.Fatal("fallback limiter should allow requests when redis is down")
	}
	if do() != http.StatusTooManyRequests {
		t.Fatal("fallback limiter should still enforce the limit")
	}
}

// TestRateLimiterMiddlewareFromConfig 验证中间件入口从配置读取阈值；未配置时兜底默认值。
func TestRateLimiterMiddlewareFromConfig(t *testing.T) {
	old := rdb
	rdb = nil // 无 Redis → 纯单机模式
	t.Cleanup(func() { rdb = old })

	// 配置 rate_limit: 2
	setTestConfig(t, "rate_limit: 2\n")
	h := RateLimiterMiddleware(http.HandlerFunc(okHandler))
	do := func() int { return doReq(h, "GET", "/", map[string]string{"X-Forwarded-For": "5.5.5.5"}).Code }
	if do() != http.StatusOK || do() != http.StatusOK {
		t.Fatal("first two should pass")
	}
	if do() != http.StatusTooManyRequests {
		t.Fatal("third should be limited (rate_limit=2)")
	}

	// 未配置 rate_limit → 兜底 defaultRateLimit(100)，正常请求不应被拦
	setTestConfig(t, "")
	h2 := RateLimiterMiddleware(http.HandlerFunc(okHandler))
	for i := 0; i < 5; i++ {
		if code := doReq(h2, "GET", "/", map[string]string{"X-Forwarded-For": "6.6.6.6"}).Code; code != http.StatusOK {
			t.Fatalf("req %d with default limit: code = %d, want 200", i, code)
		}
	}
}
