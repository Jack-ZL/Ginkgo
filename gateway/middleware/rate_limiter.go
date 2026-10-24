package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"ginkgo/config"
)

// defaultRateLimit 是配置缺失时的兜底限流阈值（次/分钟）。
// 若 config.yaml 未配置 rate_limit，viper 返回 0——照此执行会拒绝所有请求，
// 因此必须兜底为一个合理的默认值。
const defaultRateLimit = 100

// rateLimitKeyPrefix 是分布式限流在 Redis 中的 key 前缀。
// 完整 key 形如 "ginkgo:rate_limit:10.9.8.7"，前缀便于运维按模式扫描/清理。
const rateLimitKeyPrefix = "ginkgo:rate_limit:"

// ---------------------------------------------------------------------------
// 实现一：Redis 分布式限流（首选）
// ---------------------------------------------------------------------------

// rateLimitScript 是限流用的 Lua 脚本，在 Redis 服务端原子执行：
//
//	local current = redis.call('INCR', KEYS[1])      -- 计数 +1
//	if current == 1 then                             -- 第一次请求（新窗口）
//	    redis.call('EXPIRE', KEYS[1], ARGV[1])       -- 设置窗口过期时间
//	end
//	return {current, redis.call('TTL', KEYS[1])}     -- 返回当前计数和剩余秒数
//
// 【为什么必须用 Lua 脚本】"INCR 后再 EXPIRE"是两步操作，若分开执行，
// 两步之间 Redis 恰好崩溃/网络中断，key 将永不过期，对应 IP 被永久限流。
// Lua 脚本在 Redis 中原子执行，杜绝了这种竞态。这也是 Redis 官方推荐的
// 固定窗口限流实现方式。
//
// 【固定窗口算法】每个整分钟窗口独立计数，窗口过期自动清零。
// 优点是简单高效；缺点是窗口边界处可能出现 2 倍瞬时流量
// （前一窗口末尾 + 后一窗口开头），对一般业务可接受。
var rateLimitScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
    redis.call('EXPIRE', KEYS[1], tonumber(ARGV[1]))
end
return {current, redis.call('TTL', KEYS[1])}
`)

// RedisRateLimiter 基于 Redis 的分布式限流器。
//
// 【为什么需要分布式限流】单机内存限流的计数存在进程里：
//   - 部署多个网关实例时，每个实例独立计数，实际放行量 = 阈值 × 实例数；
//   - 网关重启后计数丢失。
//
// 把计数放到 Redis 后，所有网关实例共享同一份计数，限流全局精确。
//
// 【降级策略】Redis 不可用（未配置/宕机/网络故障）时，
// 自动退化为 fallback 指向的单机内存限流器——宁可限流不精确，也不影响可用性。
type RedisRateLimiter struct {
	requests int           // 时间窗口内允许的最大请求数
	window   time.Duration // 时间窗口长度（固定 1 分钟）
	fallback *RateLimiter  // Redis 不可用时退化的单机内存限流器
}

// Middleware 返回分布式限流中间件。
func (rrl *RedisRateLimiter) Middleware(next http.Handler) http.Handler {
	// 预先把降级路径包装好：Redis 不可用时直接调用这个 handler
	fallbackHandler := rrl.fallback.Middleware(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redis 未初始化：直接走单机限流
		if rdb == nil {
			fallbackHandler.ServeHTTP(w, r)
			return
		}

		ip := clientIP(r)
		key := rateLimitKeyPrefix + ip

		// 在 Redis 中原子执行 INCR + EXPIRE，返回 [当前计数, 窗口剩余秒数]
		res, err := rateLimitScript.Run(ctx, rdb, []string{key}, int(rrl.window.Seconds())).Slice()
		if err != nil {
			// Redis 调用失败（宕机/超时等）：降级为单机限流，保证请求仍能通过
			fallbackHandler.ServeHTTP(w, r)
			return
		}

		// Lua 返回的数值统一反序列化为 int64
		count, _ := res[0].(int64)
		ttl, _ := res[1].(int64)
		if count > int64(rrl.requests) {
			retryAfter := ttl
			if retryAfter < 1 {
				retryAfter = 1 // TTL 可能为 -1（无过期）或 0，兜底 1 秒
			}
			tooManyRequests(w, retryAfter)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// 实现二：单机内存限流（Redis 不可用时的降级方案，也可独立使用）
// ---------------------------------------------------------------------------

// RateLimiter 简单的滑动窗口限流器（按客户端 IP 计数，单机内存版）。
//
// 【工作原理】
//   - counts 记录每个客户端 IP 在当前时间窗口内已发出的请求数；
//   - 请求到达时先检查该 IP 的计数是否已达上限 requests：
//     达到则拒绝（429 Too Many Requests），否则计数 +1 并放行；
//   - 每个放行的请求都会安排一个定时任务（time.AfterFunc），
//     在 interval 之后把计数 -1，实现"配额随时间逐步释放"的滑动窗口效果。
//
// 【局限】计数存于进程内存：重启丢失、多实例不共享。
// 因此它只作为 RedisRateLimiter 的降级方案存在（见上方说明）。
type RateLimiter struct {
	requests int            // 时间窗口内允许的最大请求数
	interval time.Duration  // 时间窗口长度（如 1 分钟）
	counts   map[string]int // 每个客户端 IP 在当前窗口内的请求计数
	mu       sync.Mutex     // 保护 counts 的并发访问（HTTP 请求由多个 goroutine 并发处理）
}

// NewRateLimiter 创建单机内存限流器。
// requests：窗口内允许的最大请求数；interval：窗口长度。
func NewRateLimiter(requests int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: requests,
		interval: interval,
		counts:   make(map[string]int),
	}
}

// Middleware 返回限流中间件（包装 next 处理器）。
//
// 实现要点（均为历史 bug 修复后的行为）：
//  1. 只在读写 counts 的短暂临界区内加锁，下游处理不持锁（避免请求串行化）；
//  2. 计数归零时删除 map 条目（避免长期运行内存泄漏）。
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r) // 提取客户端真实 IP（详见 clientIP 函数注释）

		// ---- 临界区：检查并更新计数，立即释放锁 ----
		rl.mu.Lock()
		if rl.counts[ip] >= rl.requests {
			rl.mu.Unlock()
			tooManyRequests(w, int64(rl.interval.Seconds()))
			return
		}
		rl.counts[ip]++
		rl.mu.Unlock()
		// ---- 临界区结束，下游处理不再持锁 ----

		// 安排 interval 之后释放本次请求占用的配额
		time.AfterFunc(rl.interval, func() {
			rl.mu.Lock()
			defer rl.mu.Unlock()
			rl.counts[ip]--
			if rl.counts[ip] <= 0 {
				delete(rl.counts, ip) // 清理归零条目，防止 map 无限增长
			}
		})

		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// 中间件入口与公共工具函数
// ---------------------------------------------------------------------------

// RateLimiterMiddleware 创建限流中间件实例并返回。
//
// 限流规则来自 config.yaml：
//   - requests：配置项 rate_limit（每分钟最大请求数），未配置时兜底为 defaultRateLimit；
//   - window：固定 1 分钟。
//
// 运行模式自动选择：
//   - InitCache() 已初始化 Redis → Redis 分布式限流（多实例共享计数），
//     Redis 故障时每次请求自动降级为单机限流；
//   - 未配置 Redis → 纯单机内存限流。
//
// 注意：每次调用都会创建独立的限流器实例，
// 因此在 main.go 中只应调用一次 g.Use(middleware.RateLimiterMiddleware)。
func RateLimiterMiddleware(next http.Handler) http.Handler {
	requests := config.GetRateLimit()
	if requests <= 0 {
		requests = defaultRateLimit // 配置缺失兜底，避免 0 阈值拒绝一切请求
	}
	window := time.Minute
	local := NewRateLimiter(requests, window)
	rrl := &RedisRateLimiter{requests: requests, window: window, fallback: local}
	return rrl.Middleware(next)
}

// tooManyRequests 返回统一的 429 JSON 响应。
// retryAfter 告知客户端多少秒后重试（HTTP 标准 Retry-After 头）。
func tooManyRequests(w http.ResponseWriter, retryAfter int64) {
	w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"code":429,"message":"Rate limit exceeded"}`))
}

// clientIP 从请求中提取客户端真实 IP。
//
// 【历史 bug 修复】旧版直接使用 r.RemoteAddr，其格式为 "IP:端口"
// （如 "127.0.0.1:52344"）。同一客户端每次连接的源端口都不同，
// 导致计数分散到无数个 key 上，限流形同虚设。
//
// 【提取优先级】
//  1. X-Forwarded-For：经过代理/负载均衡器时追加的标准转发头，
//     格式为 "客户端IP, 代理1, 代理2"，取第一个即真实客户端；
//  2. X-Real-IP：Nginx 等常用的简化转发头；
//  3. RemoteAddr：无代理的直连场景，用 net.SplitHostPort 去掉端口号。
//
// 安全提示：X-Forwarded-For 可被客户端伪造。生产环境应在受信任的
// 边缘代理（如最外层 LB）处统一重写该头，再交给网关。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// 取逗号分隔列表中的第一个 IP（最初的客户端）
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// RemoteAddr 为 "IP:端口" 格式，剥离端口只保留 IP
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr // 兜底：非标准格式（如 unix socket）原样返回
}
