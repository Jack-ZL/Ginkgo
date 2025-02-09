package plugin

import "net/http"

// Plugin 插件接口
type Plugin interface {
	Apply(http.Handler) http.Handler
}

// PluginManager 插件管理器
type PluginManager struct {
	plugins []Plugin
}

// NewPluginManager 初始化插件管理器
func NewPluginManager() *PluginManager {
	return &PluginManager{plugins: []Plugin{}}
}

// AddPlugin 添加插件
func (pm *PluginManager) AddPlugin(p Plugin) {
	pm.plugins = append(pm.plugins, p)
}

// ApplyPlugins 应用所有插件
func (pm *PluginManager) ApplyPlugins(handler http.Handler) http.Handler {
	for _, p := range pm.plugins {
		handler = p.Apply(handler)
	}
	return handler
}
