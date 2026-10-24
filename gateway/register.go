package gateway

import (
	"net/http"
	"strconv"

	"ginkgo/config"
	"ginkgo/gateway/service"
)

// 本文件实现"配置驱动"的路由注册：
// 路由规则不再硬编码在 main.go 里，而是来自 config.yaml 的 routes 段，
// 增删转发规则只需改配置、无需改代码。

// RegisterProxyRoutes 根据配置批量注册反向代理路由。
//
// 每条规则（config.RouteConfig）注册为一个"前缀匹配"的代理处理器：
//   - Path 为前缀：如 "/api/v1" 可匹配 /api/v1、/api/v1/resource、/api/v1/user/42；
//     请求以原始完整路径转发给后端（不改写路径）；
//   - Methods 非空时只允许列出的 HTTP 方法（其余方法由 mux 返回 405）；
//     Methods 为空表示允许所有方法。
//
// 处理器内部的转发流程（每个请求）：
//  1. 通过 ServiceDiscovery 按策略（轮询/随机）挑选一个健康后端；无可用后端返回 503；
//  2. 从 ProxyPool 取出（或首次创建）该后端的反向代理；服务地址非法返回 502；
//  3. 由反向代理转发请求，并把后端响应原样写回客户端。
func (g *Gateway) RegisterProxyRoutes(routes []config.RouteConfig, sd *service.ServiceDiscovery, pool *ProxyPool) {
	for _, rt := range routes {
		if rt.Path == "" {
			continue // 跳过没写 path 的非法规则
		}

		// 为每条规则生成独立的代理处理器（闭包捕获当前规则无需额外变量，
		// 因为处理器逻辑与具体规则无关——转发目标由负载均衡在请求时决定）
		handler := func(w http.ResponseWriter, r *http.Request) {
			// 1. 负载均衡挑选一个健康的后端服务
			svc := sd.GetService()
			if svc == nil {
				// 注册表为空或所有实例都被健康检查摘除
				writeJSONError(w, http.StatusServiceUnavailable, "No available services")
				return
			}
			// 2. 获取（或首次创建）该服务的反向代理
			proxy := pool.Get(svc.URL)
			if proxy == nil {
				// 服务地址非法（如缺少 http:// 前缀）
				writeJSONError(w, http.StatusBadGateway, "Invalid backend service URL")
				return
			}
			// 3. 转发请求，响应原路写回客户端
			proxy.ServeHTTP(w, r)
		}

		// 按是否限定方法选择注册方式
		if len(rt.Methods) > 0 {
			g.Router.PathPrefix(rt.Path).HandlerFunc(handler).Methods(rt.Methods...)
		} else {
			g.Router.PathPrefix(rt.Path).HandlerFunc(handler)
		}
	}
}

// writeJSONError 向客户端返回统一的 JSON 错误响应。
// 网关自身产生的错误（503/502 等）都走这个函数，保证对外格式一致：
//
//	{"code": <HTTP状态码>, "message": "<描述>"}
func writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"code":` + strconv.Itoa(code) + `,"message":"` + message + `"}`))
}
