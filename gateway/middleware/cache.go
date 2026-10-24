package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-redis/redis/v8"

	"ginkgo/config"
)

// 缓存中间件的全局依赖：
//   - rdb：Redis 客户端，由 InitCache() 在 main.go 加载配置之后初始化；
//   - ctx：Redis 操作使用的上下文。网关是常驻服务，缓存的读写不应随
//     单个请求取消而中断，因此使用全局 background context。
var (
	rdb *redis.Client
	ctx = context.Background()
)

// cacheTTL 缓存有效期：过期后下一次请求会重新回源后端并刷新缓存。
// 默认 10 分钟，InitCache() 会用配置项 cache.ttl（秒）覆盖。
var cacheTTL = 10 * time.Minute

// hopByHopHeaders 是不允许被缓存/还原的"逐跳"响应头。
// 这些头描述的是"单次 TCP 连接"的属性（由 net/http 自行管理），
// 缓存后重放会干扰新连接的语义（如 Content-Length 与实际写出字节冲突、
// Connection 头影响 keep-alive 行为），因此存取时都要过滤掉。
var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Transfer-Encoding":   true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Upgrade":             true,
	"Content-Length":      true, // 由 net/http 根据实际写出的字节数自动生成
	"X-Cache":             true, // 网关自己的调试头，每次现算
}

// cachedResponse 是写入 Redis 的缓存条目结构。
// 除响应体外还保存了完整的响应头（过滤逐跳头之后），
// 命中缓存时才能把后端的 Content-Type、Set-Cookie 等头部原样还原给客户端。
// （JSON 序列化时 []byte 会自动转为 base64 字符串，反序列化时还原。）
type cachedResponse struct {
	Headers map[string][]string `json:"headers"` // 过滤后的响应头
	Body    []byte              `json:"body"`    // 响应体原文
}

// InitCache 使用 config.yaml 中 redis.* 与 cache.* 配置初始化缓存子系统。
//
// 做两件事：
//  1. 创建 Redis 客户端（地址/密码/库号来自配置）；
//  2. 用配置项 cache.ttl（秒）覆盖默认的缓存有效期。
//
// 必须在 config.LoadConfig() 之后调用（见 main.go）。
//
// 注意：redis.NewClient 是惰性连接——这里并不会真正建立 TCP 连接，
// 因此即使 Redis 没有启动，网关也能正常启动；运行期缓存读写失败时，
// CacheMiddleware 会自动降级为"直接透传"（详见其注释），不影响可用性。
func InitCache() {
	cacheTTL = config.GetCacheTTL() // 缓存有效期（配置缺失时默认 600 秒）

	if addr := config.GetRedisAddr(); addr != "" {
		rdb = redis.NewClient(&redis.Options{
			Addr:     addr,                      // 例如 "localhost:6380"
			Password: config.GetRedisPassword(), // 空字符串表示无密码
			DB:       config.GetRedisDB(),       // 数据库编号，默认 0
		})
	}
	// 未配置 redis.addr：rdb 保持 nil，缓存与分布式限流自动退化为"无 Redis"模式
}

// responseRecorder 响应"录制器"：包装真实的 http.ResponseWriter，
// 在把响应写给客户端的同时，把状态码和响应体复制一份留存，
// 供处理器执行完毕后写入 Redis 缓存。
//
// 【为什么需要它】http.ResponseWriter 是"一次性"的——内容写给客户端后
// 服务端就拿不回来了。想缓存后端的真实响应，就必须在写入路径上拦截并复制。
// 这是 Go 中实现响应缓存/响应改写的标准手法。
type responseRecorder struct {
	http.ResponseWriter              // 嵌入原始 writer：未重写的方法（如 Header()）直接委托给它
	statusCode          int          // 记录响应状态码（默认 200，与 Go 隐式行为一致）
	body                bytes.Buffer // 记录响应体副本
}

// WriteHeader 拦截状态码写入：先记录，再委托给原始 writer。
func (rec *responseRecorder) WriteHeader(statusCode int) {
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}

// Write 拦截响应体写入：先复制到录制缓冲区，再写给客户端。
func (rec *responseRecorder) Write(b []byte) (int, error) {
	rec.body.Write(b) // 复制一份用于缓存
	return rec.ResponseWriter.Write(b)
}

// CacheMiddleware 缓存 GET 请求的响应，减少后端压力、加快重复请求的响应速度。
//
// 【处理流程】
//  1. 只处理 GET 请求——POST/PUT/DELETE 等有副作用，其响应不应被缓存复用；
//  2. 以完整请求 URL（路径 + 查询参数，如 "/api/v1/resource?page=2"）作为缓存 key 查询 Redis：
//     - 命中：还原缓存的响应头 + 响应体直接写回客户端，不再访问后端；
//     - 未命中：用 responseRecorder 包装 ResponseWriter 后放行给后端；
//  3. 后端返回后，仅当状态码为 2xx 且响应体非空时才写入 Redis
//     （TTL 由配置项 cache.ttl 控制，默认 10 分钟），避免把错误结果缓存下来。
//
// 【历史 bug 修复】旧版在缓存未命中后，把从 Redis 读出的 val（未命中时是 nil）
// 写回缓存——等于永远在缓存空内容，真实响应从未被缓存。
// 现通过 responseRecorder 捕获后端的真实响应再写入。
//
// 【降级策略】Redis 不可用（未启动/网络故障）时，Get/Set 都会返回错误，
// 此时直接跳过缓存逻辑、请求正常转发后端——缓存故障不影响网关可用性。
// 调试提示：响应头 X-Cache 标识本次响应来源（HIT=命中缓存 / MISS=回源）。
func CacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 非 GET 请求、或 Redis 尚未初始化：直接透传
		if r.Method != http.MethodGet || rdb == nil {
			next.ServeHTTP(w, r)
			return
		}

		key := r.URL.String() // 缓存 key：完整请求 URL（含查询参数）

		// ---- 1. 尝试读缓存 ----
		val, err := rdb.Get(ctx, key).Bytes()
		if err == nil {
			// 命中缓存：反序列化并还原响应
			var cached cachedResponse
			if json.Unmarshal(val, &cached) == nil {
				// 还原缓存的响应头（逐跳头已在存入时过滤）
				for name, values := range cached.Headers {
					for _, v := range values {
						w.Header().Add(name, v)
					}
				}
				w.Header().Set("X-Cache", "HIT") // 标记命中，便于调试
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(cached.Body)
				return
			}
			// 缓存内容损坏（反序列化失败）：删除脏数据，走回源流程
			rdb.Del(ctx, key)
		}
		// err != nil 包含两种情况：redis.Nil（key 不存在，正常未命中）
		// 和 Redis 不可用（连接失败等）。两者都继续回源，无需区分处理。

		// ---- 2. 未命中：包装响应写入器，放行给后端并"录制"响应 ----
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		w.Header().Set("X-Cache", "MISS") // 必须在写响应体之前设置头部
		next.ServeHTTP(rec, r)

		// ---- 3. 只缓存成功且非空的响应 ----
		if rec.statusCode >= 200 && rec.statusCode < 300 && rec.body.Len() > 0 {
			entry, marshalErr := json.Marshal(cachedResponse{
				Headers: filterHeaders(rec.Header()),
				Body:    rec.body.Bytes(),
			})
			if marshalErr == nil {
				// 写缓存失败不影响本次响应（客户端已收到完整内容），静默忽略即可
				_ = rdb.Set(ctx, key, entry, cacheTTL).Err()
			}
		}
	})
}

// filterHeaders 复制一份响应头并剔除逐跳头（见 hopByHopHeaders 说明）。
// 返回新 map，不修改原始 header。
func filterHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for name, values := range h {
		if hopByHopHeaders[http.CanonicalHeaderKey(name)] {
			continue
		}
		out[name] = append([]string(nil), values...)
	}
	return out
}
