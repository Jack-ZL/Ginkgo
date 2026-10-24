package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProxyPoolReusesInstances 验证代理池按服务地址缓存实例（避免每请求新建）。
func TestProxyPoolReusesInstances(t *testing.T) {
	pool := NewProxyPool()
	p1 := pool.Get("http://localhost:8081")
	p2 := pool.Get("http://localhost:8081")
	if p1 == nil {
		t.Fatal("Get returned nil for valid URL")
	}
	if p1 != p2 {
		t.Fatal("same URL should return the cached proxy instance")
	}
	if p3 := pool.Get("http://localhost:8082"); p3 == p1 {
		t.Fatal("different URL should get a different proxy")
	}
}

// TestProxyPoolInvalidURL 验证非法服务地址返回 nil（调用方据此返回 502）。
func TestProxyPoolInvalidURL(t *testing.T) {
	pool := NewProxyPool()
	for _, bad := range []string{"", "not a url", "localhost:8081", "://x"} {
		if pool.Get(bad) != nil {
			t.Errorf("Get(%q) should return nil", bad)
		}
	}
}

// TestProxyForwardsRequest 验证反向代理转发请求：保留原始路径与查询参数，
// 并把后端的响应头/响应体原样带回。
func TestProxyForwardsRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "hit")
		_, _ = io.WriteString(w, "path="+r.URL.Path+" query="+r.URL.RawQuery)
	}))
	defer backend.Close()

	proxy := NewProxyPool().Get(backend.URL)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/resource?a=1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if want := "path=/api/v1/resource query=a=1"; rec.Body.String() != want {
		t.Fatalf("body = %q, want %q", rec.Body.String(), want)
	}
	if rec.Header().Get("X-Backend") != "hit" {
		t.Fatal("backend response header should be preserved")
	}
}

// TestProxyErrorHandler502 验证后端不可达时返回统一的 502 JSON（而非 Go 默认错误页）。
func TestProxyErrorHandler502(t *testing.T) {
	backend := httptest.NewServer(http.NotFoundHandler())
	url := backend.URL
	backend.Close() // 关闭后端 → 连接拒绝

	proxy := NewProxyPool().Get(url)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":502`) {
		t.Fatalf("body = %q, want JSON with code 502", rec.Body.String())
	}
}
