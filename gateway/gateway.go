package gateway

import (
	"net/http"
)

// Gateway 网关核心结构
type Gateway struct {
	Router  *Router      // 路由管理器
	Handler http.Handler // 处理器链
}

// NewGateway 初始化网关
func NewGateway() *Gateway {
	return &Gateway{
		Router:  NewRouter(),
		Handler: http.DefaultServeMux,
	}
}

// Use 添加中间件或插件到处理器链
func (g *Gateway) Use(middleware func(http.Handler) http.Handler) {
	g.Handler = middleware(g.Handler)
}

// Start 启动网关服务
func (g *Gateway) Start(addr string) error {
	// 将路由绑定到处理器链
	g.Handler = g.Router
	return http.ListenAndServe(addr, g.Handler)
}
