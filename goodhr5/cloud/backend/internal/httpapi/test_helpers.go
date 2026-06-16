// 本文件负责封装 HTTP API 单元测试里的公共初始化方法。
package httpapi

import "testing"

// mustNewServer 为测试创建云端服务；创建失败时直接终止测试。
func mustNewServer(t *testing.T) *Server {
	t.Helper()

	// 调用服务构造函数，保证测试和真实启动路径使用同一套依赖装配逻辑。
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.systemConfigs.Save(SystemConfig{
		ConfigKey:   "system.app_config",
		ConfigValue: `{"free_daily_greet_limit":100,"email_domain_whitelist":["example.com","qq.com"]}`,
		Description: "测试环境其它配置",
		Enabled:     true,
	}); err != nil {
		t.Fatal(err)
	}
	return server
}
