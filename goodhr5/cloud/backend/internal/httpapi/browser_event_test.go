// 本文件负责验证云端只接受明确的启动、正常退出和异常退出生命周期日志。
package httpapi

import "testing"

// TestValidRuntimeEvent 验证异常退出类型不能被普通操作或伪造的运行类别混淆。
func TestValidRuntimeEvent(t *testing.T) {
	for _, item := range []struct {
		action, category string
		isError, valid   bool
	}{
		{"browser.exit", "异常退出", true, true},
		{"worker.exit", "异常退出", true, true},
		{"agent.previous_exit", "异常退出", true, true},
		{"browser.start", "运行", false, true},
		{"agent.exit", "运行", false, true},
		{"agent.exit", "异常退出", true, true},
		{"browser.exit", "运行", false, false},
		{"element.click", "异常退出", true, false},
	} {
		if validRuntimeEvent(item.action, item.category, item.isError) != item.valid {
			t.Fatalf("生命周期日志校验不正确：%+v", item)
		}
	}
}
