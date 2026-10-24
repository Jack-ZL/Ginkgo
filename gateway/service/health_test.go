package service

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestHealthCheckerProbe 验证完整生命周期：健康 -> 故障摘除 -> 恢复加回。
func TestHealthCheckerProbe(t *testing.T) {
	// 用可切换的开关模拟后端"存活/故障"，避免真的关闭服务器
	var up atomic.Bool
	up.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if up.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sm := NewServiceManager()
	sm.AddService("backend", srv.URL)
	svc, _ := sm.GetService("backend")
	hc := NewHealthChecker(sm, time.Second, time.Second, "/healthz")

	// 初始健康
	hc.CheckAll()
	if !svc.IsHealthy() {
		t.Fatal("healthy backend should stay healthy")
	}

	// 后端返回 500：自动摘除
	up.Store(false)
	hc.CheckAll()
	if svc.IsHealthy() {
		t.Fatal("failing backend should be marked unhealthy")
	}
	if got := len(sm.ListHealthyServices()); got != 0 {
		t.Fatalf("unhealthy backend should be excluded, got %d healthy", got)
	}

	// 后端恢复：自动加回
	up.Store(true)
	hc.CheckAll()
	if !svc.IsHealthy() {
		t.Fatal("recovered backend should be marked healthy again")
	}
}

// TestHealthCheckerUnreachable 验证连接拒绝（服务宕机）判定为不健康。
func TestHealthCheckerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // 立即关闭 → 连接拒绝

	sm := NewServiceManager()
	sm.AddService("dead", url)
	svc, _ := sm.GetService("dead")

	hc := NewHealthChecker(sm, time.Second, 200*time.Millisecond, "/healthz")
	hc.CheckAll()
	if svc.IsHealthy() {
		t.Fatal("unreachable service should be marked unhealthy")
	}
}

// TestHealthCheckerWrongPath 验证探测路径 404 时判定为不健康（4xx 视为故障）。
func TestHealthCheckerWrongPath(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler()) // 所有路径都 404
	defer srv.Close()

	sm := NewServiceManager()
	sm.AddService("backend", srv.URL)
	svc, _ := sm.GetService("backend")

	hc := NewHealthChecker(sm, time.Second, time.Second, "/healthz")
	hc.CheckAll()
	if svc.IsHealthy() {
		t.Fatal("404 on probe path should be treated as unhealthy")
	}
}

// TestHealthCheckerStartStop 验证后台协程的启动/停止与 Stop 的幂等性。
func TestHealthCheckerStartStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 2xx 即健康
	}))
	defer srv.Close()

	sm := NewServiceManager()
	sm.AddService("backend", srv.URL)
	svc, _ := sm.GetService("backend")
	svc.SetHealthy(false) // 先置为不健康，验证 Start 的"立即首轮探测"能把它加回

	hc := NewHealthChecker(sm, 10*time.Millisecond, time.Second, "/")
	hc.Start()
	// Start 会立即执行一轮探测，稍等确保完成
	time.Sleep(50 * time.Millisecond)
	hc.Stop()
	hc.Stop() // 幂等：重复调用不应 panic

	if !svc.IsHealthy() {
		t.Fatal("Start should probe immediately and restore healthy state")
	}
}
