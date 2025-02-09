package main

import (
	"ginkgo/config"
	"ginkgo/gateway"
	"ginkgo/gateway/middleware"
	"ginkgo/gateway/service"
	"net/http"
)

func main() {
	// 加载配置文件
	config.LoadConfig()

	// 初始化服务管理器
	serviceManager := service.NewServiceManager()
	for _, url := range config.GetServiceURLs() {
		serviceManager.AddService("service-"+url, url) // 添加服务
	}

	// 初始化服务发现（负载均衡）
	sd := service.NewServiceDiscovery(serviceManager)

	// 创建网关实例
	g := gateway.NewGateway()

	// 添加路由：将请求转发到后端服务
	g.Router.AddRoute("GET", "/api/v1/resource", func(w http.ResponseWriter, r *http.Request) {
		service := sd.GetService()
		if service == nil {
			http.Error(w, "No available services", http.StatusServiceUnavailable)
			return
		}
		// 重定向请求到后端服务
		http.Redirect(w, r, service.URL+r.URL.Path, http.StatusTemporaryRedirect)
	})

	// 应用中间件
	g.Use(middleware.AuthMiddleware)        // 认证
	g.Use(middleware.LoggerMiddleware)      // 日志
	g.Use(middleware.CacheMiddleware)       // 缓存
	g.Use(middleware.RateLimiterMiddleware) // 限流
	g.Use(middleware.ErrorHandler)          // 错误处理

	// 启动网关服务
	g.Start(":8080")
}
