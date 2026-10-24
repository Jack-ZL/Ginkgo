package service

import "testing"

// TestServiceManagerCRUD 验证服务注册表的增删查。
func TestServiceManagerCRUD(t *testing.T) {
	sm := NewServiceManager()
	sm.AddService("a", "http://a")
	sm.AddService("b", "http://b")

	if got := len(sm.ListServices()); got != 2 {
		t.Fatalf("ListServices len = %d, want 2", got)
	}
	svc, ok := sm.GetService("a")
	if !ok || svc.URL != "http://a" {
		t.Fatalf("GetService(a) = %+v, %v", svc, ok)
	}
	// 新注册的实例默认健康（乐观策略）
	if !svc.IsHealthy() {
		t.Fatal("new service should be healthy by default")
	}
	// 重复注册同名服务 = 覆盖
	sm.AddService("a", "http://a2")
	svc, _ = sm.GetService("a")
	if svc.URL != "http://a2" {
		t.Fatalf("AddService should overwrite, got %q", svc.URL)
	}

	sm.RemoveService("a")
	if _, ok := sm.GetService("a"); ok {
		t.Fatal("service should be removed")
	}
	if _, ok := sm.GetService("not-exist"); ok {
		t.Fatal("missing service should return ok=false")
	}
}

// TestListHealthyServices 验证健康过滤与按名称排序（轮询策略依赖稳定顺序）。
func TestListHealthyServices(t *testing.T) {
	sm := NewServiceManager()
	sm.AddService("svc-b", "http://b")
	sm.AddService("svc-a", "http://a")
	sm.AddService("svc-c", "http://c")
	svcB, _ := sm.GetService("svc-b")
	svcB.SetHealthy(false)

	healthy := sm.ListHealthyServices()
	if len(healthy) != 2 {
		t.Fatalf("healthy len = %d, want 2", len(healthy))
	}
	if healthy[0].Name != "svc-a" || healthy[1].Name != "svc-c" {
		t.Fatalf("healthy order = [%s %s], want [svc-a svc-c]", healthy[0].Name, healthy[1].Name)
	}
	// ListServices 应包含不健康实例
	if got := len(sm.ListServices()); got != 3 {
		t.Fatalf("ListServices len = %d, want 3 (including unhealthy)", got)
	}
}

// TestParseStrategy 验证策略解析与非法值兜底。
func TestParseStrategy(t *testing.T) {
	cases := map[string]Strategy{
		"random":      Random,
		"RANDOM":      Random,
		" random ":    Random,
		"round_robin": RoundRobin,
		"":            RoundRobin,
		"bogus":       RoundRobin, // 非法值兜底为轮询
	}
	for in, want := range cases {
		if got := ParseStrategy(in); got != want {
			t.Errorf("ParseStrategy(%q) = %v, want %v", in, got, want)
		}
	}
	if RoundRobin.String() != "round_robin" || Random.String() != "random" {
		t.Error("Strategy.String() mismatch")
	}
}

// TestDiscoveryRoundRobin 验证轮询策略按稳定顺序循环分发。
func TestDiscoveryRoundRobin(t *testing.T) {
	sm := NewServiceManager()
	sm.AddService("svc-a", "http://a")
	sm.AddService("svc-b", "http://b")
	sd := NewServiceDiscoveryWithStrategy(sm, RoundRobin)

	// ListHealthyServices 按名称排序 → 轮询序列应为 a,b,a,b
	want := []string{"http://a", "http://b", "http://a", "http://b"}
	for i, w := range want {
		if got := sd.GetService().URL; got != w {
			t.Fatalf("round %d: got %q, want %q", i, got, w)
		}
	}
}

// TestDiscoverySkipsUnhealthy 验证不健康实例不参与负载均衡。
func TestDiscoverySkipsUnhealthy(t *testing.T) {
	sm := NewServiceManager()
	sm.AddService("svc-a", "http://a")
	sm.AddService("svc-b", "http://b")
	svcA, _ := sm.GetService("svc-a")
	svcA.SetHealthy(false)

	sd := NewServiceDiscovery(sm)
	for i := 0; i < 5; i++ {
		if got := sd.GetService().URL; got != "http://b" {
			t.Fatalf("round %d: unhealthy service was selected: %q", i, got)
		}
	}
}

// TestDiscoveryNoService 验证无可用实例时返回 nil（调用方据此返回 503）。
func TestDiscoveryNoService(t *testing.T) {
	// 空注册表
	if got := NewServiceDiscovery(NewServiceManager()).GetService(); got != nil {
		t.Fatalf("empty manager: got %+v, want nil", got)
	}
	// 全部不健康
	sm := NewServiceManager()
	sm.AddService("a", "http://a")
	svc, _ := sm.GetService("a")
	svc.SetHealthy(false)
	if got := NewServiceDiscovery(sm).GetService(); got != nil {
		t.Fatalf("all unhealthy: got %+v, want nil", got)
	}
}

// TestDiscoveryRandom 验证随机策略返回的都是已注册的健康实例，且覆盖多个实例。
func TestDiscoveryRandom(t *testing.T) {
	sm := NewServiceManager()
	sm.AddService("a", "http://a")
	sm.AddService("b", "http://b")
	sd := NewServiceDiscoveryWithStrategy(sm, Random)

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		svc := sd.GetService()
		if svc == nil {
			t.Fatal("random discovery returned nil")
		}
		if svc.URL != "http://a" && svc.URL != "http://b" {
			t.Fatalf("unknown service selected: %q", svc.URL)
		}
		seen[svc.URL] = true
	}
	if !seen["http://a"] || !seen["http://b"] {
		t.Fatalf("random should cover both services in 50 draws, seen=%v", seen)
	}
}
