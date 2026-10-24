// Package plugin 实现网关的插件系统。
//
// 【插件 vs 中间件】
// 两者本质都是对 http.Handler 的包装，区别在于组织方式：
//   - 中间件：一个裸函数 func(http.Handler) http.Handler，适合无状态的简单逻辑；
//   - 插件：  实现 Plugin 接口的独立模块，可以携带自身配置与状态，
//     适合功能复杂、需要独立维护/按需启停的扩展（如灰度发布、请求改写等）。
//
// 【使用方式】（完整示例见同目录 request_id.go）
//  1. 定义一个结构体，实现 Apply(http.Handler) http.Handler 方法；
//  2. 在 main.go 中通过 gateway.UsePlugin(p) 注册到处理器链；
//     或者用本包的 PluginManager 批量管理多个插件。
package plugin

import "net/http"

// Plugin 插件接口：所有插件必须实现 Apply 方法。
//
// Apply 接收"下一个处理器"，返回"包装后的处理器"——
// 与 net/http 中间件约定完全一致，因此插件可以自由地链式组合。
type Plugin interface {
	Apply(http.Handler) http.Handler
}

// PluginManager 插件管理器：按注册顺序保存多个插件，一次性应用到处理器链。
//
// 典型用法：
//
//	pm := plugin.NewPluginManager()
//	pm.AddPlugin(plugin.RequestIDPlugin{}) // 注册插件
//	pm.AddPlugin(myCustomPlugin)
//	handler = pm.ApplyPlugins(handler)     // 统一应用
//
// 说明：网关主体（gateway.Gateway）也提供了 UsePlugin 方法逐个注册插件，
// PluginManager 则适合"插件数量多、需要集中管理"的场景。
type PluginManager struct {
	plugins []Plugin // 已注册的插件列表（保持注册顺序）
}

// NewPluginManager 创建一个空的插件管理器。
func NewPluginManager() *PluginManager {
	return &PluginManager{plugins: []Plugin{}}
}

// AddPlugin 注册一个插件。
// 注册顺序决定包装顺序：先注册的在内层、后注册的在外层（先接触请求）。
func (pm *PluginManager) AddPlugin(p Plugin) {
	pm.plugins = append(pm.plugins, p)
}

// ApplyPlugins 按注册顺序把所有插件依次应用到 handler 上，
// 返回包装完成的最终处理器链。
func (pm *PluginManager) ApplyPlugins(handler http.Handler) http.Handler {
	for _, p := range pm.plugins {
		handler = p.Apply(handler)
	}
	return handler
}
