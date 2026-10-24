package middleware

import (
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// setupCacheTest 启动内存 Redis 并按给定 YAML 补充配置，
// 调用 InitCache() 初始化中间件包的 rdb/cacheTTL，测试后自动还原。
func setupCacheTest(t *testing.T, extraYAML string) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	setTestConfig(t, "redis:\n  addr: "+mr.Addr()+"\n"+extraYAML)

	oldRdb, oldTTL := rdb, cacheTTL
	InitCache() // 从 viper 读取 redis/cache 配置
	t.Cleanup(func() {
		if rdb != nil {
			_ = rdb.Close()
		}
		rdb, cacheTTL = oldRdb, oldTTL
	})
	return mr
}

// TestCacheMissThenHit 验证核心缓存流程：
// 首次 MISS 回源并写缓存（含 TTL 与响应头），二次 HIT 不回源、头部还原。
func TestCacheMissThenHit(t *testing.T) {
	mr := setupCacheTest(t, "cache:\n  ttl: 60\n")

	calls := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend-Header", "preserved")
		_, _ = w.Write([]byte(`{"data":1}`))
	})
	h := CacheMiddleware(backend)

	// 第一次：MISS，回源
	rec := doReq(h, "GET", "/api/x?a=1", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"data":1}` {
		t.Fatalf("miss: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("miss: X-Cache = %q", rec.Header().Get("X-Cache"))
	}
	if calls != 1 {
		t.Fatalf("backend calls = %d, want 1", calls)
	}

	// 第二次：HIT，不回源，响应头/体完整还原
	rec = doReq(h, "GET", "/api/x?a=1", nil)
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("hit: X-Cache = %q", rec.Header().Get("X-Cache"))
	}
	if rec.Body.String() != `{"data":1}` {
		t.Fatalf("hit: body = %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("hit: Content-Type = %q, want restored", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Backend-Header") != "preserved" {
		t.Fatal("hit: custom backend header should be restored")
	}
	if calls != 1 {
		t.Fatalf("backend should not be called on HIT, calls = %d", calls)
	}

	// TTL 按配置生效（cache.ttl: 60 秒）
	if ttl := mr.TTL("/api/x?a=1"); ttl != 60*time.Second {
		t.Fatalf("redis TTL = %v, want 60s", ttl)
	}

	// 不同查询参数 = 不同缓存 key，需要重新回源
	rec = doReq(h, "GET", "/api/x?a=2", nil)
	if rec.Header().Get("X-Cache") != "MISS" || calls != 2 {
		t.Fatalf("different query should miss: X-Cache=%q calls=%d", rec.Header().Get("X-Cache"), calls)
	}
}

// TestCacheSkipsNonGET 验证非 GET 请求不经过缓存（有副作用的请求不可复用响应）。
func TestCacheSkipsNonGET(t *testing.T) {
	setupCacheTest(t, "")
	calls := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte("ok"))
	})
	h := CacheMiddleware(backend)

	for i := 0; i < 2; i++ {
		rec := doReq(h, "POST", "/api/x", nil)
		if rec.Header().Get("X-Cache") != "" {
			t.Fatal("POST should not touch the cache")
		}
	}
	if calls != 2 {
		t.Fatalf("backend calls = %d, want 2 (no caching for POST)", calls)
	}
}

// TestCacheSkipsNon2xx 验证错误响应（5xx）不会被缓存（历史 bug 回归：旧版会缓存空内容）。
func TestCacheSkipsNon2xx(t *testing.T) {
	mr := setupCacheTest(t, "")
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	h := CacheMiddleware(backend)

	for i := 0; i < 2; i++ {
		rec := doReq(h, "GET", "/err", nil)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, want 500", rec.Code)
		}
		if rec.Header().Get("X-Cache") != "MISS" {
			t.Fatalf("X-Cache = %q, want MISS every time (error not cached)", rec.Header().Get("X-Cache"))
		}
	}
	if mr.Exists("/err") {
		t.Fatal("error response must not be written to redis")
	}
}

// TestCacheDegradesWhenRedisDown 验证 Redis 宕机时缓存中间件自动降级为透传，不影响可用性。
func TestCacheDegradesWhenRedisDown(t *testing.T) {
	mr := setupCacheTest(t, "")
	calls := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte("ok"))
	})
	h := CacheMiddleware(backend)

	mr.Close() // 模拟 Redis 宕机
	for i := 0; i < 2; i++ {
		rec := doReq(h, "GET", "/api/x", nil)
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Fatalf("redis down: code=%d body=%q, want pass-through 200/ok", rec.Code, rec.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("backend calls = %d, want 2", calls)
	}
}

// TestCacheDisabledWithoutRedis 验证未配置 Redis 时（rdb == nil）中间件安全透传、不 panic。
func TestCacheDisabledWithoutRedis(t *testing.T) {
	setTestConfig(t, "") // 不配置 redis.addr
	oldRdb, oldTTL := rdb, cacheTTL
	rdb = nil
	InitCache() // redis.addr 为空 → rdb 保持 nil
	t.Cleanup(func() { rdb, cacheTTL = oldRdb, oldTTL })

	calls := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte("ok"))
	})
	rec := doReq(CacheMiddleware(backend), "GET", "/api/x", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("code=%d body=%q, want 200/ok", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache") != "" {
		t.Fatal("X-Cache should not be set when cache is disabled")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

// TestCacheTTLPassthrough 验证缓存过期后重新回源（利用 miniredis 的时间快进）。
func TestCacheTTLPassthrough(t *testing.T) {
	mr := setupCacheTest(t, "cache:\n  ttl: 60\n")
	calls := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte("fresh"))
	})
	h := CacheMiddleware(backend)

	doReq(h, "GET", "/api/ttl", nil) // MISS，写入缓存
	doReq(h, "GET", "/api/ttl", nil) // HIT
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 before expiry", calls)
	}

	mr.FastForward(61 * time.Second) // 快进时间使缓存过期

	rec := doReq(h, "GET", "/api/ttl", nil)
	if rec.Header().Get("X-Cache") != "MISS" || calls != 2 {
		t.Fatalf("after TTL expiry: X-Cache=%q calls=%d, want MISS/2", rec.Header().Get("X-Cache"), calls)
	}
}
