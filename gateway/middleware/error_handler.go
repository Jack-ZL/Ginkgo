package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/sirupsen/logrus"
)

// ErrorResponse 网关统一的 JSON 错误响应格式。
//   - Code:    HTTP 状态码（如 500）
//   - Message: 人类可读的错误描述
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ErrorHandler 统一错误处理中间件：捕获内层处理器抛出的 panic，
// 返回标准化的 500 JSON 响应，而不是让连接被粗暴中断。
//
// 【为什么需要它】Go 的 net/http 虽然内置了按连接粒度的 recover，
// 但 panic 发生时连接会被直接关闭，客户端收不到任何响应体，
// 难以区分"网关崩了"还是"网络断了"。这里显式捕获并返回统一错误格式。
//
// 【挂载位置】应位于中间件链最外层（即在 main.go 中最后一个 Use），
// 这样才能捕获所有内层中间件与业务处理器的 panic。
//
// 【历史 bug 修复】旧版在 JSON 编码失败时会再次 panic（错误处理本身崩溃），
// 现改为忽略编码错误——此时响应已无法挽回，记录日志即可。
func ErrorHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// defer + recover：捕获内层调用栈中任意位置抛出的 panic
		defer func() {
			if err := recover(); err != nil {
				// 用结构化日志记录 panic 内容与请求上下文，便于排查
				logger.WithFields(logrus.Fields{
					"panic":  err,
					"method": r.Method,
					"path":   r.URL.Path,
				}).Error("recovered from panic in handler chain")

				// 返回统一 500 JSON 响应。
				// 注意：若内层已写出部分响应，这里的 WriteHeader 会被 Go 忽略
				// 并打印 "superfluous WriteHeader" 日志，但不影响进程安全。
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(ErrorResponse{
					Code:    http.StatusInternalServerError,
					Message: "Internal Server Error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
