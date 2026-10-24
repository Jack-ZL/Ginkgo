package service

import (
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// HealthChecker 后端服务健康检查器。
//
// 【工作原理】后台协程每隔 interval 对所有已注册实例发起一次 HTTP GET 探测
// （请求 URL = 实例地址 + path，如 http://localhost:8081/healthz）：
//   - 探测成功（timeout 内返回 2xx/3xx）：标记为健康——若之前是故障状态则"自动加回"负载均衡；
//   - 探测失败（连接拒绝/超时/4xx/5xx）：标记为不健康——立即从负载均衡中"自动摘除"。
//
// 状态变更（健康<->故障）会输出 WARN/INFO 日志，便于运维观察后端波动。
//
// 【与旧行为的对比】没有健康检查时，宕机实例仍会被负载均衡选中，
// 客户端要承受一次失败请求（502）；有了健康检查，故障实例最多在一个
// 探测周期内被摘除，后续请求全部路由到健康实例。
type HealthChecker struct {
	manager  *ServiceManager // 被检查的服务注册表
	interval time.Duration   // 探测间隔
	path     string          // 探测路径（如 "/healthz"）
	client   *http.Client    // 带超时的探测用 HTTP 客户端
	stopCh   chan struct{}   // 写入该 channel 通知后台协程退出
	stopped  chan struct{}   // 后台协程退出后关闭，供 Stop() 等待（避免协程泄漏）
}

// NewHealthChecker 创建健康检查器。
//   - interval：探测间隔（如 10s）；
//   - timeout：单次探测的超时时间（如 3s），必须明显小于 interval；
//   - path：探测路径，后端需对该路径返回 2xx/3xx。
//
// 创建后需调用 Start() 才会开始探测。
func NewHealthChecker(manager *ServiceManager, interval, timeout time.Duration, path string) *HealthChecker {
	return &HealthChecker{
		manager:  manager,
		interval: interval,
		path:     path,
		client:   &http.Client{Timeout: timeout},
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// Start 启动后台探测协程：先立即执行一轮探测（快速发现启动时就故障的实例），
// 之后每隔 interval 重复。非阻塞，调用后立即返回。
func (hc *HealthChecker) Start() {
	go func() {
		defer close(hc.stopped) // 协程退出时通知 Stop()

		hc.CheckAll() // 启动时立即探测一轮

		ticker := time.NewTicker(hc.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				hc.CheckAll()
			case <-hc.stopCh:
				return
			}
		}
	}()
}

// Stop 停止后台探测协程，并等待其完全退出（幂等：重复调用安全）。
func (hc *HealthChecker) Stop() {
	select {
	case <-hc.stopCh:
		// 已经关闭过，不重复 close（重复 close 会 panic）
	default:
		close(hc.stopCh)
	}
	<-hc.stopped // 等待协程真正退出
}

// CheckAll 立即对所有已注册实例执行一轮探测并更新健康状态。
// 导出该方法的目的：单元测试中可以确定性地触发探测，无需等待定时器。
func (hc *HealthChecker) CheckAll() {
	for _, svc := range hc.manager.ListServices() {
		before := svc.IsHealthy()
		after := hc.probe(svc)
		svc.SetHealthy(after)

		// 仅在状态发生"翻转"时打日志，避免每轮探测都刷屏
		if before && !after {
			logrus.Warnf("health check: service %s (%s) is DOWN, removed from load balancing", svc.Name, svc.URL)
		} else if !before && after {
			logrus.Infof("health check: service %s (%s) is UP, restored to load balancing", svc.Name, svc.URL)
		}
	}
}

// probe 对单个实例发起探测请求，返回是否健康。
// 判定标准：在超时时间内收到 2xx 或 3xx 响应即为健康；
// 连接失败、超时、4xx/5xx 均视为故障。
func (hc *HealthChecker) probe(svc *Service) bool {
	resp, err := hc.client.Get(svc.URL + hc.path)
	if err != nil {
		return false // 连接拒绝 / DNS 失败 / 超时等
	}
	defer resp.Body.Close()
	// 排空并关闭响应体，让底层 TCP 连接可以被复用（Go HTTP 客户端最佳实践）
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
