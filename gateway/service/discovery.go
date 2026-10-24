package service

import (
	"math/rand"
	"strings"
	"sync/atomic"
)

// Strategy 负载均衡策略枚举。
type Strategy int

const (
	// RoundRobin 轮询：按到达顺序依次分发到每个健康实例，流量绝对均匀。
	RoundRobin Strategy = iota
	// Random 随机：每次请求随机挑选一个健康实例。实现最简单，
	// 在请求量足够大时流量分布接近均匀。
	Random
)

// ParseStrategy 把配置字符串解析为策略枚举。
// 支持 "round_robin" / "random"（大小写不敏感）；
// 无法识别的值一律兜底为 RoundRobin，保证配置写错时网关仍按合理策略工作。
func ParseStrategy(name string) Strategy {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "random":
		return Random
	default: // "round_robin"、空串、非法值
		return RoundRobin
	}
}

// String 返回策略的可读名称（用于启动日志）。
func (s Strategy) String() string {
	if s == Random {
		return "random"
	}
	return "round_robin"
}

// ServiceDiscovery 实现服务发现与负载均衡。
//
// 【工作流程】每次请求调用 GetService()：
//  1. 从 ServiceManager 获取当前所有健康实例（被健康检查摘除的不在其中）；
//  2. 按配置的策略（轮询/随机）挑选一个返回；
//  3. 无健康实例时返回 nil，调用方应向客户端返回 503。
//
// 【并发安全】轮询计数器使用 atomic.Uint64（无锁、高并发下性能稳定）；
// 健康实例列表由 ServiceManager 的读写锁保护，本结构自身无可变状态。
type ServiceDiscovery struct {
	serviceManager *ServiceManager // 底层服务注册表
	strategy       Strategy        // 负载均衡策略
	counter        atomic.Uint64   // 轮询计数器（仅 RoundRobin 策略使用）
}

// NewServiceDiscovery 创建负载均衡器，使用默认的轮询策略。
func NewServiceDiscovery(manager *ServiceManager) *ServiceDiscovery {
	return NewServiceDiscoveryWithStrategy(manager, RoundRobin)
}

// NewServiceDiscoveryWithStrategy 创建负载均衡器并指定策略。
// main.go 中根据配置项 load_balance 调用（见 config.GetLoadBalanceStrategy）。
func NewServiceDiscoveryWithStrategy(manager *ServiceManager, strategy Strategy) *ServiceDiscovery {
	return &ServiceDiscovery{serviceManager: manager, strategy: strategy}
}

// GetService 按策略选择一个健康的后端服务。
// 没有任何健康实例时返回 nil，调用方必须判空
// （通常向客户端返回 503 Service Unavailable）。
//
// 说明：Go 1.20+ 的全局 rand 已自动随机播种且并发安全，可直接使用。
func (sd *ServiceDiscovery) GetService() *Service {
	services := sd.serviceManager.ListHealthyServices()
	if len(services) == 0 {
		return nil // 无健康实例可用
	}
	switch sd.strategy {
	case Random:
		return services[rand.Intn(len(services))]
	default: // RoundRobin
		// Add(1) 返回自增后的值，减 1 得到从 0 开始的序号；
		// 对实例数取模实现循环轮询。计数器溢出回绕也不影响正确性。
		idx := sd.counter.Add(1) - 1
		return services[idx%uint64(len(services))]
	}
}
