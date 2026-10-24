package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
)

// ProxyPool 反向代理池：为每个后端服务地址缓存一个 ReverseProxy 实例。
//
// 【为什么用反向代理而不是重定向】
// 旧实现使用 http.Redirect(307) 让客户端自己重新请求后端服务，存在两个问题：
//  1. 后端服务的真实地址暴露给了客户端，违背网关"统一入口、屏蔽内部拓扑"的初衷；
//  2. 客户端需要发起第二次请求，增加延迟；且重定向后的请求不再经过网关的中间件
//     （认证、限流、日志等全部被绕过）。
//
// 反向代理（Reverse Proxy）则由网关代替客户端把请求转发给后端，
// 再把后端的响应原样写回客户端——整个过程对客户端完全透明，它只看到网关地址。
//
// 【为什么需要"池"】
// 创建 ReverseProxy 涉及 URL 解析、Director 函数构造等开销，每个请求都新建太浪费。
// 后端服务地址来自配置文件、数量有限且相对固定，因此按地址缓存实例、并发安全地复用。
type ProxyPool struct {
	mu      sync.Mutex                        // 保护 proxies map 的并发读写（HTTP 请求是多 goroutine 并发处理的）
	proxies map[string]*httputil.ReverseProxy // key: 后端服务地址，如 "http://localhost:8081"
}

// NewProxyPool 创建一个空的代理池。
func NewProxyPool() *ProxyPool {
	return &ProxyPool{proxies: make(map[string]*httputil.ReverseProxy)}
}

// Get 返回指定后端服务地址对应的反向代理；首次访问时创建并缓存。
//
// 若 serviceURL 不是合法的 URL（缺少 scheme 或 host），返回 nil，
// 调用方需要判空并向客户端返回错误响应。
func (p *ProxyPool) Get(serviceURL string) *httputil.ReverseProxy {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 命中缓存：直接复用已创建的代理实例
	if proxy, ok := p.proxies[serviceURL]; ok {
		return proxy
	}

	// 首次访问该后端：解析服务地址
	target, err := url.Parse(serviceURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil // 非法的服务地址（如空串、缺少 http:// 前缀）
	}

	// NewSingleHostReverseProxy 创建的代理会自动完成：
	//   - 把请求的 Scheme/Host 改写为目标服务地址（Path 保持不变）；
	//   - 添加 X-Forwarded-For 头，让后端知道真实客户端 IP。
	proxy := httputil.NewSingleHostReverseProxy(target)

	// 自定义错误处理：后端不可达（连接拒绝、超时等）时返回标准的 502 JSON 响应，
	// 而不是 Go 默认的纯文本错误，保持网关对外响应格式统一。
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":502,"message":"Bad Gateway: backend service unavailable"}`))
	}

	p.proxies[serviceURL] = proxy
	return proxy
}
