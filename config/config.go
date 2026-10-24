// Package config 负责网关配置的加载与读取。
//
// 配置统一存放在项目根目录的 config.yaml 文件中，使用 Viper 库解析
// （仓库中提供了 config.example.yaml 模板，复制为 config.yaml 后按需修改）。
// 其他所有模块（中间件、服务发现等）都通过本包暴露的 GetXXX 函数获取配置项，
// 避免把配置硬编码散落在代码各处。
//
// 使用方式：
//  1. 程序启动时（main.go）先调用 LoadConfig() 加载配置文件；
//  2. 之后在任意位置按需调用 GetServerAddr() / GetRoutes() / GetJWTSecret() 等函数读取配置。
//
// 【默认值约定】所有 GetXXX 函数在配置缺失时都会返回合理的默认值
// （在各函数注释中标明），保证配置文件写漏了网关也能以缺省行为启动。
package config

import (
	"time"

	"github.com/spf13/viper"
)

// RouteConfig 描述一条路由规则（对应 config.yaml 中 routes 列表的一项）。
//
// 路由采用"路径前缀匹配"：Path 为 "/api/v1" 时，
// "/api/v1"、"/api/v1/resource"、"/api/v1/user/42" 等请求都会命中该规则，
// 并以原始完整路径转发给后端服务。
type RouteConfig struct {
	Path    string   `mapstructure:"path"`    // 路径前缀，如 "/api/v1/resource"
	Methods []string `mapstructure:"methods"` // 允许的 HTTP 方法列表；为空表示允许所有方法
}

// HealthCheckConfig 描述健康检查行为（对应 config.yaml 中 health_check 段）。
type HealthCheckConfig struct {
	Enabled  bool   `mapstructure:"enabled"`  // 是否启用健康检查
	Interval int    `mapstructure:"interval"` // 探测间隔（秒）
	Timeout  int    `mapstructure:"timeout"`  // 单次探测超时（秒）
	Path     string `mapstructure:"path"`     // 探测路径，后端应对该路径返回 2xx/3xx
}

// LoadConfig 加载配置文件（config.yaml）。
//
// 查找规则：在当前工作目录（"."）下寻找名为 "config"、类型为 "yaml" 的文件。
// 注意：这意味着必须在项目根目录下启动网关，否则会找不到配置文件。
//
// 读取失败（文件不存在、YAML 格式错误等）时直接 panic 终止程序——
// 网关没有配置就无法正常工作，属于"快速失败"（fail-fast）设计。
func LoadConfig() {
	viper.SetConfigName("config") // 配置文件名（不含扩展名）
	viper.SetConfigType("yaml")   // 配置文件类型
	viper.AddConfigPath(".")      // 配置文件搜索路径：当前目录
	if err := viper.ReadInConfig(); err != nil {
		panic("Failed to read config file: " + err.Error())
	}
}

// GetServerAddr 返回网关监听地址（配置项 server.addr）。
// 未配置时默认 ":8080"（监听所有网卡的 8080 端口）。
func GetServerAddr() string {
	if addr := viper.GetString("server.addr"); addr != "" {
		return addr
	}
	return ":8080"
}

// GetServiceURLs 返回后端服务地址列表（配置项 services）。
// 网关会把这些地址注册进 ServiceManager，并在它们之间做负载均衡。
// 示例返回值：["http://localhost:8081", "http://localhost:8082"]
func GetServiceURLs() []string {
	return viper.GetStringSlice("services")
}

// GetRoutes 返回路由规则列表（配置项 routes）。
// 配置驱动路由：无需改代码即可增删转发规则，见 RouteConfig 的前缀匹配说明。
// 未配置时返回 nil（网关将只有内置端点如 /swagger/ 可用）。
func GetRoutes() []RouteConfig {
	var routes []RouteConfig
	// UnmarshalKey 把 yaml 的 routes 列表反序列化为结构体切片；
	// 解析失败（字段类型写错等）时返回 nil，由调用方按"无路由"处理
	_ = viper.UnmarshalKey("routes", &routes)
	return routes
}

// GetLoadBalanceStrategy 返回负载均衡策略名（配置项 load_balance）。
// 可选值："round_robin"（轮询，默认）/ "random"（随机）。
// 非法值由 service.ParseStrategy 兜底为轮询。
func GetLoadBalanceStrategy() string {
	if s := viper.GetString("load_balance"); s != "" {
		return s
	}
	return "round_robin"
}

// GetHealthCheckConfig 返回健康检查配置（配置项 health_check 段）。
// 缺失字段的默认值：enabled=false（不启用）、interval=10 秒、timeout=3 秒、path="/healthz"。
func GetHealthCheckConfig() HealthCheckConfig {
	var cfg HealthCheckConfig
	_ = viper.UnmarshalKey("health_check", &cfg)
	// 对缺失/非法的数值做兜底，避免 0 间隔导致疯狂探测
	if cfg.Interval <= 0 {
		cfg.Interval = 10
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3
	}
	if cfg.Path == "" {
		cfg.Path = "/healthz"
	}
	return cfg
}

// GetRateLimit 返回限流阈值（配置项 rate_limit）。
// 含义：每个客户端 IP 每分钟最多允许的请求数。
// 若配置文件中未设置该项则返回 0（限流中间件会兜底为默认值，见 rate_limiter.go）。
func GetRateLimit() int {
	return viper.GetInt("rate_limit")
}

// GetCacheTTL 返回缓存有效期（配置项 cache.ttl，单位：秒）。
// 未配置或非法（<=0）时默认 600 秒（10 分钟）。
func GetCacheTTL() time.Duration {
	sec := viper.GetInt("cache.ttl")
	if sec <= 0 {
		sec = 600
	}
	return time.Duration(sec) * time.Second
}

// GetJWTSecret 返回 JWT 签名密钥（配置项 jwt.secret）。
// 该密钥必须与认证服务（签发 Token 的一方）保持一致，否则签名校验会失败。
func GetJWTSecret() string {
	return viper.GetString("jwt.secret")
}

// GetJWTExpire 返回 JWT Token 有效期（秒，配置项 jwt.expire）。
// 供签发 Token 的认证服务使用，网关本身只做校验（过期时间编码在 Token 内部）。
func GetJWTExpire() int {
	return viper.GetInt("jwt.expire")
}

// GetRedisAddr 返回 Redis 服务器地址（配置项 redis.addr），例如 "localhost:6380"。
// 返回空字符串表示未配置 Redis——缓存与分布式限流将不可用（自动降级）。
func GetRedisAddr() string {
	return viper.GetString("redis.addr")
}

// GetRedisPassword 返回 Redis 密码（配置项 redis.password），无密码时返回空字符串。
func GetRedisPassword() string {
	return viper.GetString("redis.password")
}

// GetRedisDB 返回 Redis 数据库编号（配置项 redis.db），未配置时默认为 0。
func GetRedisDB() int {
	return viper.GetInt("redis.db")
}

// GetLogLevel 返回日志级别字符串（配置项 log.level）。
// 可选值：debug / info / warn / error；未配置时返回空字符串（由日志中间件兜底为 info）。
func GetLogLevel() string {
	return viper.GetString("log.level")
}

// GetLogFile 返回日志文件路径（配置项 log.file），例如 "logs/gateway.log"。
// 未配置时返回空字符串，此时日志输出到控制台（stdout）。
func GetLogFile() string {
	return viper.GetString("log.file")
}
