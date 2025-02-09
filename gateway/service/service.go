package service

import (
	"sync"
)

// Service 表示一个后端服务
type Service struct {
	Name string // 服务名称
	URL  string // 服务地址
}

// ServiceManager 管理所有服务
type ServiceManager struct {
	services map[string]*Service
	mu       sync.RWMutex
}

// NewServiceManager 初始化服务管理器
func NewServiceManager() *ServiceManager {
	return &ServiceManager{services: make(map[string]*Service)}
}

// AddService 添加服务
func (sm *ServiceManager) AddService(name, url string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.services[name] = &Service{Name: name, URL: url}
}

// RemoveService 移除服务
func (sm *ServiceManager) RemoveService(name string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.services, name)
}

// GetService 根据名称获取服务
func (sm *ServiceManager) GetService(name string) (*Service, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	service, exists := sm.services[name]
	return service, exists
}

// ListServices 列出所有服务
func (sm *ServiceManager) ListServices() []*Service {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	services := make([]*Service, 0, len(sm.services))
	for _, s := range sm.services {
		services = append(services, s)
	}
	return services
}
