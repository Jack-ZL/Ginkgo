// Ginkgo API Gateway 启动入口。
//
// 【启动流程】
//  1. 加载配置文件 config.yaml（所有后续初始化都依赖它）；
//  2. 初始化日志与 Redis（缓存 + 分布式限流共用）；
//  3. 注册后端服务，创建负载均衡器，启动健康检查；
//  4. 创建网关实例，按配置注册代理路由与 Swagger 文档端点；
//  5. 挂载中间件链与插件；
//  6. 启动 HTTP 服务（监听地址来自配置项 server.addr）。
//
// @title           Ginkgo API Gateway
// @version         1.0
// @description     Ginkgo API 网关的 Swagger 文档。路由由 config.yaml 的 routes 段配置驱动（路径前缀匹配），所有命中路由的请求经中间件链（限流/缓存/日志/认证）后反向代理到负载均衡选出的后端服务。文档 UI 本身（/swagger/）无需认证。
// @host            localhost:8080
// @BasePath        /
package main

import (
	"time"

	"github.com/sirupsen/logrus"

	"ginkgo/config"
	"ginkgo/gateway"
	"ginkgo/gateway/docs"
	"ginkgo/gateway/middleware"
	"ginkgo/gateway/plugin"
	"ginkgo/gateway/service"
)

func main() {
	// ========== 1. 加载配置 ==========
	// 读取项目根目录下的 config.yaml；读取失败会直接 panic（快速失败）
	config.LoadConfig()

	// ========== 2. 初始化基础设施 ==========
	// 初始化日志：级别/输出文件来自 config.yaml 的 log 段（必须在 LoadConfig 之后）
	middleware.InitLogger()
	// 初始化 Redis 客户端与缓存 TTL：来自 config.yaml 的 redis/cache 段。
	// 惰性连接——即使 Redis 未启动网关也能正常运行：
	// 缓存中间件自动降级为透传，限流自动退化为单机内存模式
	middleware.InitCache()

	// ========== 3. 服务发现、负载均衡与健康检查 ==========
	// 把配置中的所有后端服务地址注册进服务管理器
	serviceManager := service.NewServiceManager()
	for _, url := range config.GetServiceURLs() {
		serviceManager.AddService("service-"+url, url) // 以服务地址作为唯一名称
	}
	// 负载均衡器：策略来自配置项 load_balance（round_robin 轮询 / random 随机）
	strategy := service.ParseStrategy(config.GetLoadBalanceStrategy())
	sd := service.NewServiceDiscoveryWithStrategy(serviceManager, strategy)

	// 健康检查器：定期探测每个后端实例（GET <服务地址><health_check.path>），
	// 故障实例自动从负载均衡摘除，恢复后自动加回（详见 service/health.go）
	hcCfg := config.GetHealthCheckConfig()
	var hc *service.HealthChecker
	if hcCfg.Enabled {
		hc = service.NewHealthChecker(
			serviceManager,
			time.Duration(hcCfg.Interval)*time.Second,
			time.Duration(hcCfg.Timeout)*time.Second,
			hcCfg.Path,
		)
		hc.Start()
		defer hc.Stop() // 进程退出前停止探测协程，避免协程泄漏
	}

	// ========== 4. 创建网关并注册路由 ==========
	g := gateway.NewGateway()
	// 反向代理池：为每个后端服务缓存一个 ReverseProxy 实例（见 gateway/proxy.go）
	proxyPool := gateway.NewProxyPool()

	// 配置驱动路由：按 config.yaml 的 routes 段批量注册代理规则（路径前缀匹配）。
	// 增删转发规则只需改配置，无需改代码（见 gateway/register.go）
	g.RegisterProxyRoutes(config.GetRoutes(), sd, proxyPool)

	// Swagger 文档 UI：挂载到 /swagger/（免认证白名单见 middleware/auth.go）。
	// 文档内容由 swag init 扫描代码注解生成（gateway/docs/ 下的 docs.go/swagger.json）
	g.Router.PathPrefix("/swagger/").Handler(docs.SwaggerHandler())

	// ========== 5. 挂载中间件与插件 ==========
	//
	// 【执行顺序】Use 是"由内向外"包裹处理器链：最后注册的在最外层、最先执行。
	// 因此下面 5 行注册后，请求实际经过的顺序是：
	//   ErrorHandler(兜底) -> RateLimiter(限流) -> Cache(缓存) -> Logger(日志) -> Auth(认证) -> Router(路由)
	//
	// 这个顺序的设计考量：
	//   - ErrorHandler 最外层：内层任何位置的 panic 都能被捕获；
	//   - RateLimiter 在认证之前：先拦掉超量流量，避免为无效请求浪费认证开销；
	//   - Cache 在认证之后：未认证请求拿不到缓存内容，防止越权读取。
	g.Use(middleware.AuthMiddleware)        // 认证：校验 JWT Token（密钥来自 config.yaml，/swagger/ 免认证）
	g.Use(middleware.LoggerMiddleware)      // 日志：记录请求方法/路径/耗时（JSON 结构化）
	g.Use(middleware.CacheMiddleware)       // 缓存：GET 成功响应写入 Redis，TTL 由 cache.ttl 配置
	g.Use(middleware.RateLimiterMiddleware) // 限流：Redis 分布式限流（不可用时退化为单机内存限流）
	g.Use(middleware.ErrorHandler)          // 错误处理：捕获内层 panic，统一返回 500 JSON

	// 注册示例插件：为每个请求分配唯一 X-Request-ID（响应头返回、写入 context），
	// 用于跨服务日志串联。插件与中间件的差异见 gateway/plugin/plugin.go 注释
	g.UsePlugin(plugin.RequestIDPlugin{})

	// ========== 6. 启动网关 ==========
	addr := config.GetServerAddr() // 监听地址来自配置项 server.addr（默认 :8080）
	logrus.Infof("Ginkgo API Gateway starting on %s (load_balance=%s, health_check=%v, routes=%d)",
		addr, strategy, hcCfg.Enabled, len(config.GetRoutes()))
	// Start 会阻塞当前 goroutine；启动失败（如端口被占用）直接 panic
	if err := g.Start(addr); err != nil {
		panic(err)
	}
}

// proxyRouteDocs 是 Swagger 注解的载体函数。
//
// 【为什么存在这个空函数】路由处理器是 RegisterProxyRoutes 里动态创建的闭包，
// swag 工具无法把注解挂到匿名闭包上，因此用一个永不执行的具名函数承载
// 代理端点的文档注解（swag init 只扫描注释，不关心函数体）。
//
// @Summary      反向代理后端服务（示例路由 /api/v1/resource）
// @Description  实际生效的路由由 config.yaml 的 routes 段定义（路径前缀匹配）。请求经限流、缓存、认证等中间件后，由负载均衡挑选一个健康后端并透明转发，响应原路返回。响应头 X-Cache 标识缓存命中（HIT/MISS），X-Request-ID 为链路追踪 ID。
// @Tags         proxy
// @Produce      json
// @Param        Authorization header string true "JWT Token，格式：Bearer <token>（HS256，密钥为 config.yaml 的 jwt.secret）"
// @Success      200 {string} json "后端服务返回的响应内容"
// @Failure      401 {string} json "未认证：缺失或非法的 JWT Token"
// @Failure      429 {string} json "触发限流：超过 rate_limit 次/分钟"
// @Failure      502 {string} json "后端不可达或服务地址非法"
// @Failure      503 {string} json "无可用后端服务（全部被健康检查摘除）"
// @Router       /api/v1/resource [get]
func proxyRouteDocs() {}
