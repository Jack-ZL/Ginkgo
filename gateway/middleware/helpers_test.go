package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// setTestConfig 把 YAML 字符串注入 viper 全局状态，供各中间件测试使用。
// （LoadConfig 只能读当前目录的文件，测试中改用 ReadConfig 从字符串读取。）
// 测试结束后自动 Reset，避免污染其他用例。
func setTestConfig(t *testing.T, yaml string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("bad test config: %v", err)
	}
}

// okHandler 简单的放行处理器：返回 200 + "ok"。
func okHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

// doReq 用给定的中间件链处理一个请求，返回录制结果。
func doReq(h http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
