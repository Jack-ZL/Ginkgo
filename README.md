# Gingko API Gateway
Gingko API Gateway 是一个高性能、可扩展的 API 网关，旨在为微服务架构提供统一的入口和管理。它支持多种中间件、插件系统以及服务发现与负载均衡功能。

* Files Summary:
 
ginkgo/  
├── main.go                  # 入口文件  
├── config/  
│   ├── config.go            # 配置管理  
├── gateway/  
│   ├── gateway.go           # 网关核心逻辑  
│   ├── router.go            # 请求路由  
│   ├── middleware/          # 中间件（认证、限流、缓存等）  
│   │   ├── auth.go  
│   │   ├── rate_limiter.go  
│   │   ├── cache.go    
│   │   ├── logger.go  
│   │   ├── error_handler.go  
│   ├── plugin/              # 插件系统  
│   │   ├── plugin.go  
│   ├── service/             # 服务发现与负载均衡  
│   │   ├── service.go  
│   │   ├── discovery.go  
│   ├── docs/                # API文档生成  
│   │   ├── swagger.go  
├── README.md                # 项目说明文档  