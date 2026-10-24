package middleware

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"ginkgo/config"
)

// logger 指向 logrus 的全局标准日志器（StandardLogger）。
//
// 【为什么用标准日志器而不是包内私有实例】
// 网关的多个包都需要打日志（middleware、service 的健康检查等）。
// 统一使用 logrus.StandardLogger()，InitLogger() 一次配置即可全局生效，
// 其他包直接调用 logrus.Infof/Warnf 等包级函数就能共享级别与输出目标。
var logger = logrus.StandardLogger()

func init() {
	// 兜底默认值：JSON 格式 + info 级别 + 控制台输出。
	// init() 在配置文件加载之前执行，所以这里只是"保底配置"；
	// 真实的级别与输出目标由 InitLogger() 根据 config.yaml 覆盖。
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)
	logger.SetOutput(os.Stdout)
}

// InitLogger 根据 config.yaml 中 log.* 配置初始化全局日志器。
//
// 支持的配置项：
//   - log.level：debug / info / warn / error（非法值或未配置时回退 info）；
//   - log.file： 日志文件路径（如 "logs/gateway.log"），父目录不存在时自动创建；
//     该项为空时日志输出到控制台（stdout）。
//
// 必须在 config.LoadConfig() 之后调用（见 main.go）。
func InitLogger() {
	// ---- 1. 设置日志级别 ----
	levelStr := config.GetLogLevel()
	if levelStr == "" {
		levelStr = "info" // 未配置时的默认级别
	}
	level, err := logrus.ParseLevel(levelStr)
	if err != nil {
		// 配置了非法级别（如 "verbose"）：告警并回退到 info，不让网关起不来
		logger.Warnf("invalid log level %q in config, fallback to info", levelStr)
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// ---- 2. 设置输出目标：文件或控制台 ----
	file := config.GetLogFile()
	if file == "" {
		logger.SetOutput(os.Stdout) // 未配置文件路径：输出到控制台
		return
	}
	// 自动创建日志父目录（如 logs/），避免因目录不存在导致打开文件失败
	if dir := filepath.Dir(file); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Warnf("failed to create log dir %q: %v, fallback to stdout", dir, err)
			logger.SetOutput(os.Stdout)
			return
		}
	}
	// 以追加模式打开日志文件（不存在则创建），保证重启不丢历史日志
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logger.Warnf("failed to open log file %q: %v, fallback to stdout", file, err)
		logger.SetOutput(os.Stdout)
		return
	}
	logger.SetOutput(f)
	logger.Infof("logging to file %s at level %s", file, level)
}

// LoggerMiddleware 记录每个请求的访问日志（方法、路径、耗时等）。
//
// 计时点在 next.ServeHTTP 前后，因此 duration 包含了内层所有中间件
// 与后端服务处理的总耗时，可用于观察接口性能。
func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r) // 先完成请求处理
		// 处理完毕后输出一条 JSON 结构化日志
		logger.WithFields(logrus.Fields{
			"method":      r.Method,                   // HTTP 方法
			"path":        r.URL.Path,                 // 请求路径
			"query":       r.URL.RawQuery,             // 查询参数
			"remote_addr": r.RemoteAddr,               // 客户端地址（IP:端口）
			"user_agent":  r.UserAgent(),              // 客户端 UA
			"duration":    time.Since(start).String(), // 总耗时
		}).Info("Request handled")
	})
}
