// Package gateway 实现 API 网关的核心功能，包括：
//   - Router：请求路由（基于 gorilla/mux）
//   - Gateway：网关主体，负责组织"中间件链 + 路由器"并启动 HTTP 服务
//   - ProxyPool：反向代理池（见 proxy.go）
//
// 【中间件链的工作原理】
// 每个中间件都是签名为 func(http.Handler) http.Handler 的函数：
// 它接收"下一个处理器"，返回一个"包装后的新处理器"。
// 多次调用 Use() 会把中间件像洋葱一样层层包裹在路由器外面：
//
//	请求 --> ErrorHandler --> RateLimiter --> Cache --> Logger --> Auth --> Router --> 反向代理 --> 后端服务
//
// 越后注册的中间件位于洋葱越外层：最先接触请求、最后接触响应。
package gateway

import (
	"net/http"

	"ginkgo/gateway/plugin"
)

// Gateway 网关核心结构
type Gateway struct {
	Router  *Router      // 路由管理器：保存所有路由规则，是中间件链的最内层处理器
	Handler http.Handler // 处理器链：中间件层层包裹 Router 之后的最终处理器，交给 ListenAndServe
}

// NewGateway 初始化网关。
//
// 【关键点】Handler 的初始值就是 Router 本身。
// 这样后续每次 Use() 都是把中间件包在"路由器外面"，
// 保证请求穿过整个中间件链后，最终流入 Router 中注册的路由规则。
//
// 【历史 bug 修复】旧版本这里初始化为 http.DefaultServeMux，
// 且 Start() 里又用 g.Handler = g.Router 把中间件链整个覆盖掉，
// 导致所有 Use() 挂载的中间件全部失效。现已修复。
func NewGateway() *Gateway {
	router := NewRouter()
	return &Gateway{
		Router:  router,
		Handler: router, // 初始处理器链 = 路由器本身
	}
}

// Use 向处理器链添加一个中间件。
//
// 每调用一次，就把当前处理器链再包一层：
//
//	g.Handler = middleware(g.Handler)
//
// 因此后注册的中间件在外层、先执行（洋葱模型）。
func (g *Gateway) Use(middleware func(http.Handler) http.Handler) {
	g.Handler = middleware(g.Handler)
}

// UsePlugin 向处理器链添加一个插件。
//
// 插件与中间件本质相同（都是包装 http.Handler），
// 区别在于插件实现了 plugin.Plugin 接口，通常带有自身配置/状态，
// 便于作为独立扩展模块组织和复用（示例见 gateway/plugin/request_id.go）。
func (g *Gateway) UsePlugin(p plugin.Plugin) {
	g.Handler = p.Apply(g.Handler)
}

// Start 启动网关 HTTP 服务，阻塞当前 goroutine 直到服务退出。
//
// addr 为监听地址，例如 ":8080"（监听所有网卡的 8080 端口）。
// 直接使用 Use()/UsePlugin() 组装好的 g.Handler，中间件链完整生效。
func (g *Gateway) Start(addr string) error {
	return http.ListenAndServe(addr, g.Handler)
}
