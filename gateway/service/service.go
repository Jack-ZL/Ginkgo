// Package service 实现后端服务的管理、发现、负载均衡与健康检查。
//
// 核心组件：
//   - ServiceManager：  服务注册表，维护"服务名 -> 服务实例"的映射（增删查）；
//   - ServiceDiscovery：负载均衡器，从注册表中按策略（轮询/随机）挑选一个健康服务；
//   - HealthChecker：   健康检查器，定期探测后端服务，故障实例自动摘除、恢复后自动加回。
//
// 三者的协作关系：
//
//	HealthChecker --更新健康状态--> ServiceManager <--筛选健康实例-- ServiceDiscovery <--每次请求-- 路由处理器
package service

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Service 表示一个后端服务实例。
type Service struct {
	Name string // 服务名称（注册表中的唯一 key）
	URL  string // 服务地址，如 "http://localhost:8081"

	// healthy 标记实例是否健康（可参与负载均衡）。
	// 使用 atomic.Bool 而非普通 bool + 锁：
	// 健康检查协程写、每个请求的处理协程读，原子变量在这种
	// "单写多读"场景下无锁且并发安全，读路径零开销。
	// 新注册的实例默认健康（乐观策略），首轮探测后会被修正。
	healthy atomic.Bool
}

// NewService 创建一个服务实例，初始状态为健康。
func NewService(name, url string) *Service {
	s := &Service{Name: name, URL: url}
	s.healthy.Store(true)
	return s
}

// IsHealthy 返回实例当前的健康状态。
func (s *Service) IsHealthy() bool {
	return s.healthy.Load()
}

// SetHealthy 更新实例的健康状态（由 HealthChecker 调用）。
func (s *Service) SetHealthy(healthy bool) {
	s.healthy.Store(healthy)
}

// ServiceManager 管理所有已注册的后端服务。
//
// 【并发安全】网关运行期可能动态增删服务（如未来的服务注册功能），
// 而读操作（ListServices/GetService）发生在每个请求的处理路径上，
// 因此用 sync.RWMutex 保护：多个读可并行，写时独占。
type ServiceManager struct {
	services map[string]*Service // key: 服务名
	mu       sync.RWMutex        // 读写锁，保护 services 的并发访问
}

// NewServiceManager 初始化一个空的服务管理器。
func NewServiceManager() *ServiceManager {
	return &ServiceManager{services: make(map[string]*Service)}
}

// AddService 注册（或覆盖）一个后端服务，初始状态为健康。
// name 为服务唯一标识，url 为服务地址。
func (sm *ServiceManager) AddService(name, url string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.services[name] = NewService(name, url)
}

// RemoveService 按名称移除一个服务（例如下线某台后端实例）。
func (sm *ServiceManager) RemoveService(name string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.services, name)
}

// GetService 按名称精确查找服务。
// 第二个返回值 exists 表示是否找到，调用方应检查后再使用。
func (sm *ServiceManager) GetService(name string) (*Service, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	service, exists := sm.services[name]
	return service, exists
}

// ListServices 返回当前所有已注册服务（含不健康实例）的快照切片。
// 返回的是新分配的切片（元素为指针），调用方可安全遍历，
// 不会与后续的注册/移除操作产生数据竞争。
//
// 【为什么排序】Go 的 map 遍历顺序是随机的，若直接返回，
// 每次快照中实例的顺序都不同，轮询（RoundRobin）策略将无法稳定轮转。
// 按服务名排序保证快照顺序确定，轮询才是真轮询。
func (sm *ServiceManager) ListServices() []*Service {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	services := make([]*Service, 0, len(sm.services))
	for _, s := range sm.services {
		services = append(services, s)
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services
}

// ListHealthyServices 返回所有健康实例的快照切片。
// 负载均衡只应从这个列表中选择目标——被健康检查摘除的故障实例不会出现，
// 从而避免把请求转发到已宕机的后端。
func (sm *ServiceManager) ListHealthyServices() []*Service {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	services := make([]*Service, 0, len(sm.services))
	for _, s := range sm.services {
		if s.IsHealthy() {
			services = append(services, s)
		}
	}
	// 排序原因同 ListServices：保证轮询策略的顺序稳定
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services
}
