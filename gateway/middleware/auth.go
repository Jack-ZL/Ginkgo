// Package middleware 实现网关的各类中间件（认证、限流、缓存、日志、错误处理）。
//
// 所有中间件遵循统一签名：func(next http.Handler) http.Handler，
// 即"接收下一个处理器，返回包装后的处理器"，便于在 gateway.Use() 中链式组装。
package middleware

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"ginkgo/config"
)

// authExcludePrefixes 列出无需认证即可访问的路径前缀。
//
// 【为什么需要白名单】中间件链对"所有"请求生效，若不排除，
// Swagger 文档页面本身也会要求携带 JWT——浏览器直接打开 /swagger/ 会收到 401，
// 文档就没法看了。文档 UI 属于公开元信息，不含业务数据，放行是安全的。
var authExcludePrefixes = []string{
	"/swagger/", // Swagger API 文档 UI
}

// isAuthExcluded 判断请求路径是否命中免认证白名单（前缀匹配）。
func isAuthExcluded(path string) bool {
	for _, prefix := range authExcludePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// AuthMiddleware 校验请求携带的 JWT Token，拦截未认证请求。
//
// 【校验流程】
//  1. 从请求头 Authorization 中提取 Token，支持两种写法：
//     - "Bearer <token>"（OAuth2 标准格式，推荐）
//     - "<token>"（裸 Token，兼容旧客户端）
//  2. 使用 config.yaml 中配置的签名密钥（jwt.secret）解析并验证签名；
//  3. 限定签名算法必须是 HS256——防止攻击者把 Token 头部的 alg 改成
//     "none"（跳过签名）或 RS256（用公钥当 HMAC 密钥伪造签名），
//     这是 JWT 最常见的"算法混淆"安全漏洞；
//  4. 校验失败（缺失 Token / 签名错误 / 已过期）返回 401，通过则放行给下一个处理器。
//
// 【历史 bug 修复】
//   - 旧版把密钥硬编码为 "your-secret-key"，现改为从 config.GetJWTSecret() 读取；
//   - 旧版使用已停止维护的 dgrijalva/jwt-go，现迁移到官方接手的 golang-jwt/jwt/v5。
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 0. 白名单路径（如 Swagger 文档）直接放行，不做认证
		if isAuthExcluded(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// 1. 提取并清理 Authorization 请求头（去掉 "Bearer " 前缀和首尾空白）
		authHeader := r.Header.Get("Authorization")
		tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if tokenString == "" {
			unauthorized(w, "missing authorization token")
			return
		}

		// 2. 解析并校验 Token
		token, err := jwt.Parse(tokenString,
			// keyFunc：返回验签所用的密钥。HS256 是对称签名算法，密钥即共享密钥本身。
			func(token *jwt.Token) (interface{}, error) {
				return []byte(config.GetJWTSecret()), nil
			},
			// 安全加固：只接受 HS256 算法签名的 Token
			jwt.WithValidMethods([]string{"HS256"}),
		)

		// 3. 解析失败或 Token 无效（签名错误、exp 过期等）都视为认证失败
		if err != nil || !token.Valid {
			unauthorized(w, "invalid or expired token")
			return
		}

		// 4. 认证通过，放行
		next.ServeHTTP(w, r)
	})
}

// unauthorized 返回统一的 401 JSON 响应。
// reason 为失败原因（仅包含本文件内写死的固定字符串，不含用户输入，无注入风险）。
func unauthorized(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"code":401,"message":"Unauthorized: ` + reason + `"}`))
}
