package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// contextKey 是 context.Value 键的私有类型。
// Go context 最佳实践：用包内私有类型作 key，避免与其他包的 key 冲突。
type contextKey string

// RequestIDKey 是请求 ID 在 context.Context 中的键。
// 内层处理器可通过 r.Context().Value(plugin.RequestIDKey) 取出当前请求的 ID，
// 写入自己的业务日志，实现跨服务的日志串联。
const RequestIDKey contextKey = "request-id"

// RequestIDPlugin 示例插件：为每个请求分配一个全局唯一的请求 ID。
//
// 【功能】
//   - 若请求已携带 X-Request-ID 头（例如由上游网关/客户端传入），直接沿用；
//   - 否则用加密安全随机数生成一个 32 位十六进制 ID；
//   - ID 会写入响应头 X-Request-ID，并存入请求 context。
//
// 【用途】请求 ID 贯穿网关与后端服务的所有日志。排查线上问题时，
// 用户只需提供响应头里的 ID，即可在日志系统中检索出这次请求的完整链路
// （轻量级分布式追踪思想）。
//
// 本文件同时演示了"如何实现一个插件"：
// 定义结构体 → 实现 Apply(http.Handler) http.Handler → 在 main.go 中 UsePlugin 注册。
type RequestIDPlugin struct{}

// Apply 实现 Plugin 接口：包装 next 处理器，在处理请求前注入请求 ID。
func (RequestIDPlugin) Apply(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 优先沿用上游传入的请求 ID，保证跨系统链路 ID 一致
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		// 写入响应头：客户端/上游代理可凭此 ID 报障与追踪
		w.Header().Set("X-Request-ID", id)
		// 写入 context：内层处理器与中间件可读取
		ctx := context.WithValue(r.Context(), RequestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// newRequestID 生成 16 字节加密安全随机数的十六进制字符串（32 个字符）。
// 读取随机数失败时（极端情况，如系统熵源异常）退化为纳秒时间戳，
// 保证函数永远返回可用的 ID。
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
