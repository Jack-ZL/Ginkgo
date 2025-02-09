package docs

import (
	"github.com/swaggo/http-swagger"
	"net/http"
)

// SwaggerHandler 返回Swagger文档处理器
func SwaggerHandler() http.Handler {
	return httpSwagger.WrapHandler
}
