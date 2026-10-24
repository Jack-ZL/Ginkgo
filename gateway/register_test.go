package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ginkgo/config"
	"ginkgo/gateway/service"
)

// newTestBackend 创建一个回显请求路径的测试后端。
func newTestBackend(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"backend":true,"path":"`+r.URL.Path+`"}`)
	}))
}

// TestRegisterProxyRoutes 验证配置驱动路由：前缀匹配、原始路径转发、方法限制。
func TestRegisterProxyRoutes(t *testing.T) {
	backend := newTestBackend(t)
	defer backend.Close()

	sm := service.NewServiceManager()
	sm.AddService("b1", backend.URL)
	sd := service.NewServiceDiscovery(sm)

	g := NewGateway()
	g.RegisterProxyRoutes([]config.RouteConfig{
		{Path: "/api/v1", Methods: []string{"GET"}},
	}, sd, NewProxyPool())

	// 前缀匹配：/api/v1/resource/42 命中规则 "/api/v1"，并以原始路径转发
	rec := httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/resource/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"path":"/api/v1/resource/42"`) {
		t.Fatalf("body = %q, want original path forwarded", rec.Body.String())
	}

	// 方法限制：POST 不在 methods 列表 → mux 返回 405
	rec = httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST code = %d, want 405", rec.Code)
	}

	// 前缀之外的路径 → 404
	rec = httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/other", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other path code = %d, want 404", rec.Code)
	}
}

// TestRegisterProxyRoutesAllMethods 验证 methods 为空时允许所有方法。
func TestRegisterProxyRoutesAllMethods(t *testing.T) {
	backend := newTestBackend(t)
	defer backend.Close()

	sm := service.NewServiceManager()
	sm.AddService("b1", backend.URL)
	sd := service.NewServiceDiscovery(sm)

	g := NewGateway()
	g.RegisterProxyRoutes([]config.RouteConfig{{Path: "/api"}}, sd, NewProxyPool())

	for _, method := range []string{"GET", "POST", "DELETE"} {
		rec := httptest.NewRecorder()
		g.Handler.ServeHTTP(rec, httptest.NewRequest(method, "/api/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s code = %d, want 200", method, rec.Code)
		}
	}
}

// TestRegisterProxyRoutesNoServices 验证无可用后端时返回 503 JSON。
func TestRegisterProxyRoutesNoServices(t *testing.T) {
	sd := service.NewServiceDiscovery(service.NewServiceManager())
	g := NewGateway()
	g.RegisterProxyRoutes([]config.RouteConfig{{Path: "/api"}}, sd, NewProxyPool())

	rec := httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":503`) {
		t.Fatalf("body = %q, want JSON with code 503", rec.Body.String())
	}
}

// TestRegisterProxyRoutesUnhealthyExcluded 验证被健康检查摘除的实例不会被选中：
// 唯一后端不健康时等价于"无可用服务"→ 503。
func TestRegisterProxyRoutesUnhealthyExcluded(t *testing.T) {
	sm := service.NewServiceManager()
	sm.AddService("b1", "http://127.0.0.1:1") // 不可达地址
	svc, _ := sm.GetService("b1")
	svc.SetHealthy(false) // 模拟健康检查已将其摘除
	sd := service.NewServiceDiscovery(sm)

	g := NewGateway()
	g.RegisterProxyRoutes([]config.RouteConfig{{Path: "/api"}}, sd, NewProxyPool())

	rec := httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503 (unhealthy backend must be excluded)", rec.Code)
	}
}

// TestRegisterProxyRoutesInvalidURL 验证服务地址非法时返回 502。
func TestRegisterProxyRoutesInvalidURL(t *testing.T) {
	sm := service.NewServiceManager()
	sm.AddService("bad", "not-a-valid-url") // 缺少 scheme，ProxyPool.Get 会返回 nil
	sd := service.NewServiceDiscovery(sm)

	g := NewGateway()
	g.RegisterProxyRoutes([]config.RouteConfig{{Path: "/api"}}, sd, NewProxyPool())

	rec := httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", rec.Code)
	}
}

// TestRegisterProxyRoutesSkipsEmptyPath 验证 path 为空的非法规则被跳过（不 panic）。
func TestRegisterProxyRoutesSkipsEmptyPath(t *testing.T) {
	g := NewGateway()
	g.RegisterProxyRoutes(
		[]config.RouteConfig{{Path: ""}, {Path: "/ok"}},
		service.NewServiceDiscovery(service.NewServiceManager()),
		NewProxyPool(),
	)
	// 空 path 若被注册会匹配所有请求；跳过后根路径应为 404
	rec := httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404 (empty path rule should be skipped)", rec.Code)
	}
}
