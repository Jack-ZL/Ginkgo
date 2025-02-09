package middleware

import (
	"github.com/sirupsen/logrus"
	"net/http"
	"time"
)

var logger = logrus.New()

func init() {
	logger.SetFormatter(&logrus.JSONFormatter{}) // 结构化日志
	logger.SetLevel(logrus.InfoLevel)            // 设置日志级别
}

// LoggerMiddleware 记录请求日志
func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.WithFields(logrus.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"duration": time.Since(start),
		}).Info("Request handled")
	})
}
