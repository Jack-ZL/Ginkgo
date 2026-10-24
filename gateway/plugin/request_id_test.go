package plugin

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestRequestIDGenerates 验证插件生成 32 位十六进制 ID，写入响应头与 context。
func TestRequestIDGenerates(t *testing.T) {
	var ctxID any
	h := RequestIDPlugin{}.Apply(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = r.Context().Value(RequestIDKey)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	id := rec.Header().Get("X-Request-ID")
	if !requestIDPattern.MatchString(id) {
		t.Fatalf("X-Request-ID = %q, want 32-char lowercase hex", id)
	}
	if ctxID != id {
		t.Fatalf("context value %v != header id %q", ctxID, id)
	}
}

// TestRequestIDReusesUpstream 验证沿用上游传入的 X-Request-ID（跨系统链路一致）。
func TestRequestIDReusesUpstream(t *testing.T) {
	h := RequestIDPlugin{}.Apply(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Request-ID", "upstream-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "upstream-123" {
		t.Fatalf("X-Request-ID = %q, want upstream-123", got)
	}
}

// TestRequestIDUniqueness 验证 100 个请求生成的 ID 互不重复。
func TestRequestIDUniqueness(t *testing.T) {
	h := RequestIDPlugin{}.Apply(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
		id := rec.Header().Get("X-Request-ID")
		if seen[id] {
			t.Fatalf("duplicate request id %q at iteration %d", id, i)
		}
		seen[id] = true
	}
}

// headerPlugin 是测试用的最小插件实现：给响应加一个固定头。
type headerPlugin struct{ name, value string }

func (p headerPlugin) Apply(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(p.name, p.value)
		next.ServeHTTP(w, r)
	})
}

// TestPluginManager 验证 PluginManager 按注册顺序应用所有插件。
func TestPluginManager(t *testing.T) {
	pm := NewPluginManager()
	pm.AddPlugin(headerPlugin{"X-First", "1"})
	pm.AddPlugin(headerPlugin{"X-Second", "2"})

	h := pm.ApplyPlugins(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("inner"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Header().Get("X-First") != "1" || rec.Header().Get("X-Second") != "2" {
		t.Fatalf("plugin headers missing: %v", rec.Header())
	}
	if rec.Body.String() != "inner" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
