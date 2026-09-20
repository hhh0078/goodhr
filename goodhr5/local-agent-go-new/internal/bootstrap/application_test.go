// Package bootstrap 文件作用：验证应用更新请求只触发一次安全退出信号。
package bootstrap

import "testing"

// TestRequestShutdownIsIdempotent 验证重复更新回调不会因重复关闭通道而崩溃。
func TestRequestShutdownIsIdempotent(t *testing.T) {
	application := &Application{shutdown: make(chan struct{})}
	application.RequestShutdown()
	application.RequestShutdown()
	select {
	case <-application.shutdown:
	default:
		t.Fatal("更新请求没有触发主程序安全退出")
	}
}
