package gateway

import (
	"github.com/gorilla/mux"
	"net/http"
)

// Router 封装路由逻辑
type Router struct {
	*mux.Router
}

// NewRouter 初始化路由
func NewRouter() *Router {
	return &Router{mux.NewRouter()}
}

// AddRoute 添加路由规则
func (r *Router) AddRoute(method, path string, handler http.HandlerFunc) {
	r.HandleFunc(path, handler).Methods(method)
}

// ServeHTTP 实现http.Handler接口
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.Router.ServeHTTP(w, req)
}
