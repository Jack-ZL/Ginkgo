// 本文件实现网关的请求路由：根据请求的 Method + Path 找到对应的处理函数。
// 底层使用三方库 gorilla/mux（Go 生态最流行的 HTTP 路由器之一），
// 相比标准库 http.ServeMux，mux 支持按方法过滤、路径参数、正则匹配等高级特性。
package gateway

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Router 封装路由逻辑。
// 通过嵌入 *mux.Router 直接复用其全部路由能力（HandleFunc、PathPrefix 等），
// 同时对外提供更适合网关场景的简化 API（AddRoute）。
type Router struct {
	*mux.Router
}

// NewRouter 初始化一个空的路由器。
func NewRouter() *Router {
	return &Router{mux.NewRouter()}
}

// AddRoute 添加一条路由规则：
// 当请求的 HTTP 方法为 method、路径为 path 时，交给 handler 处理。
//
// 示例：
//
//	r.AddRoute("GET", "/api/v1/resource", myHandler)
//
// 未匹配到任何规则的请求由 mux 默认返回 404 Not Found。
func (r *Router) AddRoute(method, path string, handler http.HandlerFunc) {
	r.HandleFunc(path, handler).Methods(method)
}

// ServeHTTP 实现 http.Handler 接口，使 Router 可以直接作为
// 中间件链的最内层处理器（见 gateway.go 中 NewGateway 的说明）。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.Router.ServeHTTP(w, req)
}
