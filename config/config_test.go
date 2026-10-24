package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// testConfigYAML 覆盖所有配置项的测试样例
const testConfigYAML = `
server:
  addr: ":9090"
services:
  - "http://localhost:8081"
  - "http://localhost:8082"
load_balance: "random"
health_check:
  enabled: true
  interval: 5
  timeout: 2
  path: "/health"
routes:
  - path: "/api/v1/resource"
    methods: ["GET"]
  - path: "/api/v2"
rate_limit: 50
redis:
  addr: "localhost:6380"
  password: "pw"
  db: 2
cache:
  ttl: 30
jwt:
  secret: "test-secret"
  expire: 7200
log:
  level: "debug"
  file: "logs/test.log"
`

// TestLoadConfigAndGetters 验证 LoadConfig 能读取当前目录的 config.yaml，
// 且所有 getter 返回配置值。
func TestLoadConfigAndGetters(t *testing.T) {
	// LoadConfig 只认"当前目录下的 config.yaml"，因此写入临时目录并切换过去
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(testConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Cleanup(viper.Reset)

	LoadConfig()

	if got := GetServerAddr(); got != ":9090" {
		t.Errorf("GetServerAddr() = %q, want :9090", got)
	}
	if got := GetServiceURLs(); len(got) != 2 || got[0] != "http://localhost:8081" {
		t.Errorf("GetServiceURLs() = %v", got)
	}
	if got := GetLoadBalanceStrategy(); got != "random" {
		t.Errorf("GetLoadBalanceStrategy() = %q", got)
	}
	hc := GetHealthCheckConfig()
	if !hc.Enabled || hc.Interval != 5 || hc.Timeout != 2 || hc.Path != "/health" {
		t.Errorf("GetHealthCheckConfig() = %+v", hc)
	}
	routes := GetRoutes()
	if len(routes) != 2 {
		t.Fatalf("GetRoutes() len = %d, want 2", len(routes))
	}
	if routes[0].Path != "/api/v1/resource" || len(routes[0].Methods) != 1 || routes[0].Methods[0] != "GET" {
		t.Errorf("routes[0] = %+v", routes[0])
	}
	if routes[1].Path != "/api/v2" || len(routes[1].Methods) != 0 {
		t.Errorf("routes[1] = %+v", routes[1])
	}
	if got := GetRateLimit(); got != 50 {
		t.Errorf("GetRateLimit() = %d", got)
	}
	if got := GetRedisAddr(); got != "localhost:6380" {
		t.Errorf("GetRedisAddr() = %q", got)
	}
	if got := GetRedisPassword(); got != "pw" {
		t.Errorf("GetRedisPassword() = %q", got)
	}
	if got := GetRedisDB(); got != 2 {
		t.Errorf("GetRedisDB() = %d", got)
	}
	if got := GetCacheTTL(); got != 30*time.Second {
		t.Errorf("GetCacheTTL() = %v", got)
	}
	if got := GetJWTSecret(); got != "test-secret" {
		t.Errorf("GetJWTSecret() = %q", got)
	}
	if got := GetJWTExpire(); got != 7200 {
		t.Errorf("GetJWTExpire() = %d", got)
	}
	if got := GetLogLevel(); got != "debug" {
		t.Errorf("GetLogLevel() = %q", got)
	}
	if got := GetLogFile(); got != "logs/test.log" {
		t.Errorf("GetLogFile() = %q", got)
	}
}

// TestDefaults 验证配置完全缺失时各 getter 的兜底默认值。
func TestDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	if got := GetServerAddr(); got != ":8080" {
		t.Errorf("default ServerAddr = %q, want :8080", got)
	}
	if got := GetLoadBalanceStrategy(); got != "round_robin" {
		t.Errorf("default strategy = %q", got)
	}
	if got := GetCacheTTL(); got != 600*time.Second {
		t.Errorf("default CacheTTL = %v, want 10m", got)
	}
	hc := GetHealthCheckConfig()
	if hc.Enabled || hc.Interval != 10 || hc.Timeout != 3 || hc.Path != "/healthz" {
		t.Errorf("default HealthCheckConfig = %+v", hc)
	}
	if got := GetRoutes(); got != nil {
		t.Errorf("default Routes = %v, want nil", got)
	}
	if got := GetServiceURLs(); len(got) != 0 {
		t.Errorf("default ServiceURLs = %v", got)
	}
}

// TestLoadConfigPanicsWhenMissing 验证缺少配置文件时快速失败（panic）。
func TestLoadConfigPanicsWhenMissing(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir()) // 空目录，没有 config.yaml

	defer func() {
		if recover() == nil {
			t.Fatal("LoadConfig should panic when config.yaml is missing")
		}
	}()
	LoadConfig()
}
