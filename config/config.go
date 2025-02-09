package config

import (
	"github.com/spf13/viper"
)

// LoadConfig 加载配置文件（config.yaml）
func LoadConfig() {
	viper.SetConfigName("config") // 配置文件名称
	viper.SetConfigType("yaml")   // 配置文件类型
	viper.AddConfigPath(".")      // 配置文件路径
	if err := viper.ReadInConfig(); err != nil {
		panic("Failed to read config file: " + err.Error())
	}
}

// GetServiceURLs 从配置文件中获取后端服务地址列表
func GetServiceURLs() []string {
	return viper.GetStringSlice("services")
}

// GetRateLimit 从配置文件中获取限流规则
func GetRateLimit() int {
	return viper.GetInt("rate_limit")
}
