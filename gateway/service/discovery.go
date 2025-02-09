package service

import (
	"math/rand"
	"sync"
)

// ServiceDiscovery 实现负载均衡和服务发现
type ServiceDiscovery struct {
	serviceManager *ServiceManager
	mu             sync.RWMutex
}

// NewServiceDiscovery 初始化服务发现
func NewServiceDiscovery(manager *ServiceManager) *ServiceDiscovery {
	return &ServiceDiscovery{serviceManager: manager}
}

// GetService 随机选择一个可用服务
func (sd *ServiceDiscovery) GetService() *Service {
	sd.mu.RLock()
	defer sd.mu.RUnlock()
	services := sd.serviceManager.ListServices()
	if len(services) == 0 {
		return nil
	}
	return services[rand.Intn(len(services))]
}
