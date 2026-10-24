# Gingko API Gateway

Gingko API Gateway 是一个高性能、可扩展的 API 网关，旨在为微服务架构提供统一的入口和管理。它支持多种中间件、插件系统以及服务发现与负载均衡功能。

> 当前为学习型项目，完整展示了 API 网关的核心概念：路由、中间件链、反向代理、负载均衡、健康检查、限流、缓存、插件、API 文档。

## 功能特性

- **配置驱动路由**：路由规则写在 `config.yaml` 的 `routes` 段（路径前缀匹配 + 方法白名单），无需改代码即可增删路由
- **反向代理**：基于 `httputil.ReverseProxy` 将请求透明转发到后端（代理实例按服务地址池化复用），客户端只感知网关地址
- **中间件链**：认证、日志、缓存、限流、错误处理，通过 `Gateway.Use()` 依次挂载
- **JWT 认证**：校验请求头 `Authorization` 中的 JWT Token（仅接受 HS256，密钥来自配置；`/swagger/` 路径免认证）
- **分布式限流**：Redis 可用时使用 Lua 脚本原子计数（多网关实例共享配额），Redis 故障时自动退化为单机内存滑动窗口限流；按客户端 IP 计数（支持 `X-Forwarded-For` / `X-Real-IP`），超限返回 `429` 并携带 `Retry-After`
- **响应缓存**：基于 Redis 缓存 GET 请求的成功响应（完整响应头 + 响应体，自动过滤逐跳头），TTL 由 `cache.ttl` 配置；Redis 故障时自动降级为透传（`X-Cache: HIT/MISS` 标识来源）
- **健康检查**：后台协程定期探测后端 `/healthz`，故障实例自动摘除、恢复后自动加回（logrus 记录状态变迁）
- **负载均衡策略**：支持 `round_robin`（轮询，默认）与 `random`（随机），由 `load_balance` 配置项选择；仅在健康实例中选取
- **结构化日志**：基于 logrus，JSON 格式记录请求方法、路径、耗时；级别/文件由配置控制
- **错误兜底**：`recover` 捕获 panic，统一返回 JSON 错误响应
- **插件系统**：`Plugin` 接口 + `PluginManager`，已通过 `Gateway.UsePlugin()` 接入主流程，内置示例插件 `RequestIDPlugin`（为每个请求分配 `X-Request-ID`，便于日志链路追踪）
- **API 文档**：swaggo 注解已编写并生成 docs 包，启动后访问 `http://localhost:8080/swagger/index.html` 查看 Swagger UI（免认证）
- **配置管理**：基于 Viper 读取 `config.yaml`，监听地址、限流、缓存 TTL、健康检查等均可配置；仓库提供 `config.example.yaml` 模板
- **单元测试**：覆盖 config / service / health / plugin / gateway / proxy / register / 全部中间件（Redis 相关用例基于 miniredis，无需真实 Redis），`go test ./...` 一键运行

## 技术栈

| 依赖 | 用途 |
| --- | --- |
| Go 1.26 | 开发语言 |
| gorilla/mux | HTTP 路由 |
| golang-jwt/jwt/v5 | JWT 认证（官方维护的 JWT 库） |
| go-redis/redis/v8 | 缓存 + 分布式限流 |
| sirupsen/logrus | 结构化日志 |
| spf13/viper | 配置文件管理 |
| swaggo/swag + http-swagger | Swagger API 文档生成与展示 |
| alicebob/miniredis（仅测试） | 内存 Redis，用于限流/缓存单元测试 |

## 架构与请求流程

```
客户端请求
   │
   ▼
Gateway (server.addr，默认 :8080)
   │
   ├─ RequestIDPlugin        # 插件：分配 X-Request-ID（响应头返回、写入 context）
   ├─ ErrorHandler           # panic 兜底，统一返回 500 JSON
   ├─ RateLimiterMiddleware  # IP 限流（Redis 分布式计数，故障退化内存滑动窗口）
   ├─ CacheMiddleware        # Redis 缓存：GET 命中直接返回（X-Cache: HIT/MISS）
   ├─ LoggerMiddleware       # 记录请求日志（JSON 结构化，级别/文件由 config 控制）
   ├─ AuthMiddleware         # JWT 校验（密钥来自 config，仅接受 HS256；/swagger/ 免认证）
   │
   ▼
Router (gorilla/mux)
   │  按 config.yaml 的 routes 做路径前缀 + 方法匹配
   ▼
ServiceDiscovery.GetService()   # round_robin / random，仅从健康实例中选取
   │        ▲
   │        │ HealthChecker 后台协程定期探测 /healthz，自动摘除/恢复实例
   ▼
ProxyPool.Get(serviceURL)       # 反向代理（按服务地址缓存 ReverseProxy 实例）
   │
   ▼
后端服务 (config.yaml 中的 services 列表)，响应原路写回客户端
```

## 目录结构

```
ginkgo/
├── main.go                  # 入口：加载配置、注册服务/路由、挂载中间件、启动健康检查与 HTTP 服务（含 swag 注解）
├── config.yaml              # 本地配置（已 gitignore，从模板复制）
├── config.example.yaml      # 配置模板（含全部配置项注释）
├── config/
│   ├── config.go            # 配置管理（Viper 读取，所有配置项带默认值）
│   └── config_test.go
├── gateway/
│   ├── gateway.go           # 网关核心（中间件链 + 插件挂载 + 启动 HTTP 服务）
│   ├── router.go            # 请求路由（封装 gorilla/mux）
│   ├── proxy.go             # 反向代理池（按后端地址缓存 ReverseProxy 实例）
│   ├── register.go          # 配置驱动路由注册（前缀匹配 + 方法白名单 + 统一 JSON 错误）
│   ├── docs/                # swag init 生成的 Swagger 文档包
│   │   ├── swagger.go       # Swagger 文档处理器
│   │   ├── docs.go / swagger.json / swagger.yaml
│   ├── middleware/          # 中间件（认证、限流、缓存等，均带 *_test.go）
│   │   ├── auth.go          # JWT 认证（密钥读配置，限定 HS256，/swagger/ 免认证）
│   │   ├── rate_limiter.go  # 限流（Redis Lua 分布式 + 内存滑动窗口降级）
│   │   ├── cache.go         # Redis 响应缓存（完整头 + 体，TTL 可配置，故障降级）
│   │   ├── logger.go        # logrus 结构化日志（级别/文件读配置）
│   │   ├── error_handler.go # 统一错误处理（panic recover）
│   ├── plugin/              # 插件系统
│   │   ├── plugin.go        # Plugin 接口 + PluginManager
│   │   ├── request_id.go    # 示例插件：为每个请求分配 X-Request-ID
│   ├── service/             # 服务发现与负载均衡
│   │   ├── service.go       # ServiceManager：服务注册/移除/查询 + 健康状态标记
│   │   ├── discovery.go     # ServiceDiscovery：round_robin / random 策略
│   │   ├── health.go        # HealthChecker：后台定期探活，自动摘除/恢复故障实例
├── README.md                # 项目说明文档
```

## 配置说明（config.yaml）

首次使用请复制模板：`cp config.example.yaml config.yaml`。完整配置项：

```yaml
server:
  addr: ":8080"               # 网关监听地址（":端口" 或 "IP:端口"）

services:                     # 后端服务地址列表（网关在它们之间做负载均衡）
  - "http://localhost:8081"
  - "http://localhost:8082"

load_balance: "round_robin"   # 负载均衡策略：round_robin（默认）/ random

health_check:                 # 健康检查：故障实例自动摘除、恢复后自动加回
  enabled: true
  interval: 10                # 探测间隔（秒）
  timeout: 3                  # 单次探测超时（秒）
  path: "/healthz"            # 探测路径（后端需返回 2xx/3xx）

routes:                       # 路由规则（path 为前缀匹配；methods 留空 = 允许所有方法）
  - path: "/api/v1/resource"
    methods: ["GET"]

rate_limit: 100               # 每个客户端 IP 每分钟最多请求数

redis:                        # Redis（缓存 + 分布式限流；不可用时自动降级）
  addr: "localhost:6380"
  password: ""
  db: 0

cache:
  ttl: 600                    # 缓存有效期（秒），默认 600

jwt:
  secret: "your-secret-key"   # JWT 签名密钥（需与认证服务一致）
  expire: 3600                # Token 有效期（秒）

log:
  level: "info"               # 可选：debug, info, warn, error
  file: "logs/gateway.log"    # 留空则输出到控制台
```

## 快速开始

1. 准备依赖：启动 Redis（默认 `localhost:6380`）和至少一个后端服务（如 `localhost:8081`，需实现 `/healthz` 探活接口，或将 `health_check.path` 改为 `/`）
2. 复制并按需修改配置：`cp config.example.yaml config.yaml`
3. 在项目根目录运行：

```bash
go run main.go
```

4. 网关监听 `:8080`（可通过 `server.addr` 修改），携带有效 JWT 访问：

```bash
curl -H "Authorization: ******" http://localhost:8080/api/v1/resource
```

5. 浏览器打开 Swagger UI（免认证）：<http://localhost:8080/swagger/index.html>

> 提示：Token 需使用 HS256 算法、以 `jwt.secret` 配置的密钥签发。若本机 8080 端口已被其他程序（如 nginx）占用，网关会绑定到 IPv6 或启动失败，请先释放端口。

## 使用教程（开发者接入指南）

本章面向**第一次使用 Ginkgo 的开发者**，手把手演示如何把网关接入你自己的项目。

### 0. 接入方式总览

**先明确项目定位**——Ginkgo 属于哪种类型的软件？很多新用户会在这里困惑：

| 使用方式 | 是否适用 | 说明 |
| --- | --- | --- |
| ① 在里面开发业务（类似 beego 框架） | ❌ | 它不是业务开发框架，不要把业务代码写进本仓库 |
| ② 作为依赖包 import 进自己的项目 | ❌ | 模块名为本地 `ginkgo`，未发布、也未按库设计（入口是 `main.go` 完整程序） |
| ③ **独立进程部署在你的服务前面** | ✅ | 编译出二进制单独运行，你的项目**零代码改动**，只通过 `config.yaml` 声明后端地址、转发路径、限流/缓存/JWT 等 |

也就是说，你的项目和 Ginkgo 的关系不是「代码依赖」，而是「**网络拓扑上的上下游**」——就像用 nginx 做反向代理，没人会把 nginx import 进自己的项目。

**一个直观的类比：把 Ginkgo 当成 MySQL 那样的基础设施软件来用。**

- 它和你的项目是**两回事**：独立进程、单独部署，双方只有网络交互（HTTP），没有任何代码耦合
- 你的程序**不需要引入它、也不需要为它改一行代码**，只改它的 `config.yaml` 告诉它「你的服务在哪、转发什么路径」——就像应用通过连接串使用 MySQL 一样
- 它挂了不影响你的服务本身（只是统一入口断了），和「MySQL 挂了应用进程还在」同理

与 MySQL 唯一的差别：Ginkgo 是开源的，**可以**二次开发（fork 后加自定义中间件/插件、改认证逻辑，见 §7）。但那是「改造网关本身」的进阶玩法，普通使用者完全不需要——就像绝大多数人用 MySQL 也从不去改 MySQL 源码。

一句话总结：**把它当成一个「用 Go 写的、可配置的 nginx」来用就对了。**

> 如果确实想「库式复用」（比如只把限流中间件拿进自己的 Go 项目），需要 fork 本仓库并把 go.mod 的模块路径改成可发布的地址（如 `github.com/<你的用户名>/Ginkgo`）后再 `go get`，但这不是它的设计初衷。

Ginkgo 是一个**独立运行的进程**，接入你的项目不需要改你业务的任何代码，典型部署拓扑：

```
                        ┌──────────────────────────────┐
 客户端 ──HTTP──▶       │   Ginkgo 网关 (:8080)         │
 (App/浏览器/curl)      │  认证·限流·缓存·日志·负载均衡   │
                        └──────┬───────────────┬───────┘
                               │               │
                        ┌──────▼─────┐  ┌──────▼─────┐
                        │ 你的服务 A  │  │ 你的服务 B  │   ◀── config.yaml 的 services 列表
                        │ :8081      │  │ :8082      │       （同一个服务的多个实例也可以）
                        └────────────┘  └────────────┘
                               ▲
                        ┌──────┴─────┐
                        │   Redis    │   ◀── 可选：缓存 + 分布式限流（不启动也能跑，自动降级）
                        └────────────┘
```

两种接入姿势：

| 方式 | 适用场景 | 需要做什么 |
| --- | --- | --- |
| **A. 独立部署（推荐）** | 给现有服务加统一入口 | 只改 `config.yaml`，零代码 |
| **B. 二次开发** | 需要自定义中间件/插件/认证逻辑 | fork 本仓库，改 `main.go`（见「二次开发扩展点」） |

下面按方式 A 完整走一遍。

### 1. 环境准备

- Go 1.26 及以上（`go version` 确认）
- Redis（可选，推荐）：用于响应缓存与分布式限流。**不启动 Redis 网关也能正常运行**——缓存自动降级为透传、限流退化为单机内存模式
- 你自己的后端 HTTP 服务（任意语言实现，见下一步的接口要求）

### 2. 让你的后端服务满足两个约定

**约定 ①：提供健康检查接口。** 网关会定期 `GET <服务地址><health_check.path>`（默认 `/healthz`），返回 2xx/3xx 视为健康。如果你的服务没有专用探活接口，把 `health_check.path` 改成 `/` 或关闭 `health_check.enabled` 即可。

**约定 ②：业务路径与 routes 配置对应。** 网关按「路径前缀」转发：配置 `path: "/api/v1"` 后，`/api/v1/xxx` 的所有请求都会**原样带着完整路径**转发给后端，后端只需正常处理自己的路由。

以 Go 为例，一个最小可用的后端服务（保存为 `backend/main.go`，`go run` 启动在 8081）：

```go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	// 约定①：健康检查接口（网关探活用，返回 200 即可）
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	// 约定②：业务接口（路径与网关 routes 配置的前缀对应）
	http.HandleFunc("/api/v1/resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"message":"hello from backend","query":%q}`, r.URL.RawQuery)
	})
	fmt.Println("backend listening on :8081")
	http.ListenAndServe(":8081", nil)
}
```

> 已有项目（Spring Boot / Express / Gin …）无需改造，只要补一个 `/healthz` 路由即可接入。

### 3. 修改配置文件

```bash
cp config.example.yaml config.yaml   # config.yaml 已被 gitignore，不会误提交密钥
```

按你的环境改这 4 处（其余保持默认即可跑通）：

```yaml
services:                        # ① 改成你自己的服务地址（可写多个实例）
  - "http://localhost:8081"

routes:                          # ② 声明要转发哪些路径（前缀匹配）
  - path: "/api/v1"              #    /api/v1/** 的请求全部转发到 services
    methods: ["GET", "POST"]     #    留空 methods = 允许所有方法

jwt:
  secret: "换成你自己的随机密钥"    # ③ 与签发 Token 的一方保持一致（务必修改默认值！）

redis:
  addr: "localhost:6379"         # ④ 改成你的 Redis 地址（没有 Redis 就保持默认，自动降级）
```

### 4. 签发 JWT Token

网关只接受满足以下全部条件的 Token，否则一律 401：

- 签名算法 **HS256**（其他算法如 RS256、alg=none 会被直接拒绝）
- 签名密钥 = `config.yaml` 里的 `jwt.secret`
- 未过期（`exp` 声明为未来的 Unix 秒级时间戳）
- 通过请求头携带：`Authorization: Bearer <token>`（也兼容不带 `Bearer ` 前缀）

实际项目中 Token 应由你的**认证服务/登录接口**签发。本地调试可以用下面的 Python 一行式脚本快速生成（无需安装第三方库）：

```bash
python3 - <<'PY'
import hmac, hashlib, base64, json, time
secret = b"换成你自己的随机密钥"          # 与 config.yaml 的 jwt.secret 一致
b64 = lambda d: base64.urlsafe_b64encode(d).rstrip(b"=")
h = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
p = b64(json.dumps({"sub": "dev-user", "exp": int(time.time()) + 3600}).encode())
s = b64(hmac.new(secret, h + b"." + p, hashlib.sha256).digest())
print((h + b"." + p + b"." + s).decode())
PY
```

Go 项目推荐用 [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) 签发；也可以打开 [jwt.io](https://jwt.io) 手动生成（Algorithm 选 HS256，Secret 填你的密钥）。

### 5. 启动网关并验证

```bash
# 终端1：启动 Redis（可选）
redis-server --port 6379

# 终端2：启动你的后端服务
go run backend/main.go

# 终端3：在项目根目录启动网关（必须在根目录，程序从当前目录读 config.yaml）
cd Ginkgo && go run main.go
```

看到日志 `Ginkgo API Gateway starting on :8080 (load_balance=round_robin, health_check=true, routes=1)` 即启动成功。逐项验证：

```bash
TOKEN="<第4步生成的token>"

# ① 不带 Token → 应返回 401（认证生效）
curl -i http://localhost:8080/api/v1/resource

# ② 带 Token → 应返回 200 和你后端的响应（转发成功）
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/resource

# ③ 重复②的请求 → 响应头 X-Cache 应从 MISS 变为 HIT（缓存生效，需 Redis）
# ④ 每个响应都带 X-Request-ID → 拿它去 logs/gateway.log 里 grep 可串联整条链路
# ⑤ 浏览器打开 http://localhost:8080/swagger/index.html → API 文档（免认证）
```

到这里接入就完成了——你的客户端今后只需要把请求发到网关地址，其余（认证、限流、缓存、负载均衡）都由网关接管。

### 6. 进阶配置速查

| 想做什么 | 改哪里 |
| --- | --- |
| 增加一组转发路径 | `routes` 加一条 `- path: "/api/v2"`（前缀匹配，子路径全转发） |
| 某路径只允许特定方法 | 该条 route 的 `methods: ["GET", "POST"]` |
| 扩容后端实例 | `services` 列表追加地址，重启网关 |
| 换负载均衡策略 | `load_balance: "random"`（默认 round_robin） |
| 调整限流阈值 | `rate_limit: 100`（每 IP 每分钟请求数） |
| 调整缓存时长 | `cache.ttl: 600`（秒；仅缓存 GET 的 2xx 响应） |
| 后端没有 /healthz | `health_check.path: "/"` 或 `health_check.enabled: false` |
| 换网关端口 | `server.addr: ":9090"` |
| 日志输出到控制台 | `log.file: ""`；调低级别排查问题用 `log.level: "debug"` |
| 多网关实例共享限流 | 各实例配置**同一个** `redis.addr` 即可（限流计数存 Redis） |

### 7. 二次开发扩展点（方式 B）

- **自定义中间件**：实现 `func(next http.Handler) http.Handler` 签名的函数，在 `main.go` 中 `g.Use(你的中间件)` 挂载。注意 `Use` 是**由内向外**包裹：最后注册的在最外层、最先执行（现有顺序与设计考量见 `main.go` 第 5 步注释）
- **自定义插件**：实现 `plugin.Plugin` 接口（`Name()` + `Execute()`），用 `g.UsePlugin()` 注册；插件在最外层执行，适合做与业务无关的通用处理（参考 `gateway/plugin/request_id.go`）
- **修改认证逻辑**（如对接 OAuth2 / 多密钥）：改 `gateway/middleware/auth.go`；新增免认证路径往 `authExcludePrefixes` 里加前缀
- **更新 Swagger 文档**：改完 `main.go` 里的注解后执行 `swag init -g main.go -o gateway/docs` 重新生成

### 8. 常见问题排查（FAQ）

**启动类**

| 现象 | 原因与解决 |
| --- | --- |
| 启动即 panic：`config file not found` | 没有在**项目根目录**启动。程序从当前目录读 `config.yaml`，请 `cd` 到根目录再 `go run main.go` |
| 启动即 panic：`bind: address already in use` | 端口被占用。`lsof -i :8080` 找到占用进程（常见是 nginx），杀掉它或改 `server.addr` |
| 启动了但 curl 连不上 | 若 8080 被其他程序占用了 IPv4，网关可能只绑到了 IPv6——用 `curl "http://[::1]:8080/..."` 验证，根治办法是释放端口 |

**请求返回错误码**

| 状态码 | 含义 | 排查步骤 |
| --- | --- | --- |
| `401 missing authorization token` | 没带认证头 | 加 `-H "Authorization: Bearer <token>"`；注意 Bearer 后有空格 |
| `401 invalid or expired token` | Token 无效 | 依次检查：① 签名密钥与 `jwt.secret` 完全一致（区分大小写/空格）② 算法是 HS256 ③ `exp` 未过期 ④ Token 复制时没被截断 |
| `404 page not found` | 路径没命中任何 route | `routes` 是**前缀匹配**：确认请求路径以某条 `path` 开头；改完配置需重启网关 |
| `405 Method Not Allowed` | 方法不在白名单 | 该 route 的 `methods` 里没有你的请求方法；留空 `methods` 可放开全部方法 |
| `429 Too Many Requests` | 触发限流 | 响应头 `Retry-After` 是建议等待秒数。调试时嫌烦可临时调大 `rate_limit`；用 Redis 限流时可 `redis-cli DEL "ginkgo:rate_limit:<你的IP>"` 立即清零 |
| `502 Bad Gateway` | 后端不可达/地址非法 | ① 直接 `curl http://localhost:8081/...` 确认后端活着 ② 检查 `services` 里的地址格式（必须带 `http://` 前缀） |
| `503 Service Unavailable` | 所有后端都被健康检查摘除 | 看 `logs/gateway.log` 里的 `is DOWN` 记录；确认后端实现了 `health_check.path` 指定的接口且返回 2xx/3xx；后端恢复后**无需重启网关**，下一轮探测自动加回（日志出现 `is UP, restored`） |
| `500 Internal Server Error` | 网关内部 panic 被兜底 | 拿响应头 `X-Request-ID` 去 `logs/gateway.log` grep，定位具体请求 |

**功能不生效**

| 现象 | 原因与解决 |
| --- | --- |
| 响应头一直没有 `X-Cache`，或永远 MISS | 缓存只对 **GET** 且后端返回 **2xx** 的响应生效；确认 Redis 已启动且 `redis.addr` 正确（`redis-cli -p <端口> ping` 应返回 PONG）。验证：请求两次同一 URL，第二次应为 HIT；`redis-cli KEYS '/api*'` 能看到缓存 key |
| 缓存了不该缓存的数据 / 想强制刷新 | 缓存 key 是完整 URL（含查询参数），加个随机参数如 `?_=123` 即可绕过；或 `redis-cli DEL "<url>"` 删除；TTL 到期（默认 600 秒）自动失效 |
| 限流像是没生效（多实例各限各的） | 多网关实例必须配置**同一个 Redis**才是分布式限流；Redis 不可用时自动退化为单机内存限流（功能正常但配额不共享） |
| 负载均衡不轮询 | ① `load_balance` 是否为 `round_robin` ② 是否只有一个健康实例（另一个被摘除了，看日志）③ 缓存命中时不回源，自然看不到轮询——加不同查询参数再试 |
| Swagger 页面打不开 | 地址是 `http://localhost:8080/swagger/index.html`（注意 `/swagger/` 前后斜杠）；该路径免认证，打不开通常是网关没起来 |
| `logs/gateway.log` 没有内容 | `log.file` 留空时日志只输出到控制台；目录不存在会自动创建，检查启动用户是否有写权限 |

**排查通用套路**：① 把 `log.level` 调成 `debug` 重启 ② 看 `logs/gateway.log`（JSON 格式，含 method/path/duration/remote_addr）③ 用响应头 `X-Request-ID` 串联单次请求 ④ 绕过网关直接 curl 后端，先分清是网关问题还是后端问题。

## 运行测试

```bash
go test ./...        # 全部单元测试（Redis 用例基于 miniredis，无需真实 Redis）
go vet ./...         # 静态检查
```

修改 main.go 的 swag 注解后重新生成文档：

```bash
go install github.com/swaggo/swag/cmd/swag@latest
swag init -g main.go -o gateway/docs
```
