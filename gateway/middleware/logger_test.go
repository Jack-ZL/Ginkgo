package middleware

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// restoreLogger 测试后还原全局日志器状态，避免影响其他用例。
func restoreLogger(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		logrus.SetOutput(os.Stdout)
		logrus.SetLevel(logrus.InfoLevel)
	})
}

// TestInitLoggerToFile 验证按配置创建日志目录/文件并写入，级别生效。
func TestInitLoggerToFile(t *testing.T) {
	restoreLogger(t)
	dir := t.TempDir()
	logFile := filepath.Join(dir, "sub", "test.log") // 父目录不存在，应被自动创建
	setTestConfig(t, "log:\n  level: warn\n  file: "+logFile+"\n")

	InitLogger()

	if logrus.GetLevel() != logrus.WarnLevel {
		t.Fatalf("level = %v, want warning", logrus.GetLevel())
	}
	logrus.Warn("hello-file")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if !strings.Contains(string(data), "hello-file") {
		t.Fatalf("log file content = %q", data)
	}
	// JSON 结构化格式
	if !strings.Contains(string(data), `"level":"warning"`) {
		t.Fatalf("log should be JSON formatted: %q", data)
	}
}

// TestInitLoggerInvalidLevelFallsBack 验证非法级别回退为 info（不让网关起不来）。
func TestInitLoggerInvalidLevelFallsBack(t *testing.T) {
	restoreLogger(t)
	setTestConfig(t, "log:\n  level: bogus\n")

	InitLogger()

	if logrus.GetLevel() != logrus.InfoLevel {
		t.Fatalf("level = %v, want info (fallback)", logrus.GetLevel())
	}
}

// TestInitLoggerStdoutWhenNoFile 验证未配置文件路径时输出到控制台，级别仍生效。
func TestInitLoggerStdoutWhenNoFile(t *testing.T) {
	restoreLogger(t)
	setTestConfig(t, "log:\n  level: debug\n")

	InitLogger()

	if logrus.GetLevel() != logrus.DebugLevel {
		t.Fatalf("level = %v, want debug", logrus.GetLevel())
	}
	// 输出目标为 stdout（不 panic 即可，内容断言由 TestInitLoggerToFile 覆盖）
	logrus.Debug("to-stdout")
}

// TestLoggerMiddleware 验证访问日志中间件放行请求且不影响响应。
func TestLoggerMiddleware(t *testing.T) {
	restoreLogger(t)
	logrus.SetOutput(os.Stderr) // 避免污染测试输出断言
	h := LoggerMiddleware(http.HandlerFunc(okHandler))
	rec := doReq(h, "GET", "/api/x?a=1", nil)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("code=%d body=%q, want 200/ok", rec.Code, rec.Body.String())
	}
}
