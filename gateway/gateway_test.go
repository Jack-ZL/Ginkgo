package gateway

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// orderPlugin 测试用插件：把名字追加到响应头 X-Order 中。
type orderPlugin struct{ name string }

func (p orderPlugin) Apply(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Order", p.name)
		next.ServeHTTP(w, r)
	})
}

// TestUseBuildsChainOverRouter 是"中间件链被覆盖"历史 bug 的回归测试：
// 验证 Use 挂载的中间件确实生效，且执行顺序符合洋葱模型（后注册的先执行）。
func TestUseBuildsChainOverRouter(t *testing.T) {
	g := NewGateway()

	mw := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Order", name)
				next.ServeHTTP(w, r)
			})
		}
	}
	g.Use(mw("first"))
	g.Use(mw("second")) // 后注册 → 外层 → 先执行
	g.UsePlugin(orderPlugin{name: "plugin"})

	g.Router.AddRoute("GET", "/x", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Order", "handler")
		_, _ = w.Write([]byte("ok"))
	})

	rec := httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	// 期望顺序：plugin(最外层) -> second -> first -> handler(最内层)
	want := []string{"plugin", "second", "first", "handler"}
	if got := rec.Header().Values("X-Order"); !reflect.DeepEqual(got, want) {
		t.Fatalf("execution order = %v, want %v", got, want)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", rec.Body.String())
	}
}

// TestRouterMethodAnd404 验证路由的方法匹配与默认 404 行为。
func TestRouterMethodAnd404(t *testing.T) {
	g := NewGateway()
	g.Router.AddRoute("GET", "/only-get", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// 未注册路径 → 404
	rec := httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path: code = %d, want 404", rec.Code)
	}

	// 注册路径 + 正确方法 → 200
	rec = httptest.NewRecorder()
	g.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/only-get", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /only-get: code = %d, want 200", rec.Code)
	}
}
