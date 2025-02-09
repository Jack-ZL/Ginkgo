package middleware

import (
	"context"
	"github.com/go-redis/redis/v8"
	"net/http"
	"time"
)

var (
	rdb = redis.NewClient(&redis.Options{Addr: "localhost:6380"}) // TODO：redis connect info get from config
	ctx = context.Background()
)

// CacheMiddleware 缓存响应
func CacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.String()
		val, err := rdb.Get(ctx, key).Bytes()
		if err == nil {
			w.Write(val)
			return
		}
		next.ServeHTTP(w, r)
		rdb.Set(ctx, key, val, 10*time.Minute)
	})
}
