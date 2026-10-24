// Package docs 提供 API 文档能力：集成 swaggo/http-swagger，
// 在网关上暴露 Swagger UI 页面，展示通过注解生成的 OpenAPI 文档。
package docs

import (
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"
)

// SwaggerHandler 返回 Swagger 文档处理器。
//
// 使用方式（在 main.go 中挂载到某个路径）：
//
//	g.Router.PathPrefix("/swagger/").Handler(docs.SwaggerHandler())
//
// 注意：Swagger UI 展示的文档内容来自 `swag init` 命令扫描代码注解
// 生成的 docs 包（docs/docs.go）。本项目尚未编写注解、未生成该包，
// 因此当前挂载后页面为空——这是待完善项，而非 bug。
func SwaggerHandler() http.Handler {
	return httpSwagger.WrapHandler
}
