// Package bootstrap 文件作用：验证应用退出幂等性，以及自动回复浏览器和后台监听器共用一次下载订阅。
package bootstrap

import (
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/flow/download"
)

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

// TestReplyBrowserSharesBackgroundDownloadSubscription 验证流程接口保留正式浏览器的下载订阅，并与后台监听器共享同一份登记。
func TestReplyBrowserSharesBackgroundDownloadSubscription(t *testing.T) {
	monitor := &download.Monitor{}
	flow := &auto_reply.Flow{Browser: &replyBrowser{Monitor: monitor}}
	watcher, ok := flow.Browser.(interface {
		WatchDownload(string, string, ...string) (<-chan contract.DownloadReceipt, func(), error)
	})
	if !ok {
		t.Fatal("自动回复浏览器没有传递下载订阅能力")
	}
	const page, file = "https://example.invalid/chat", "https://example.invalid/resume.pdf"
	_, stop, err := watcher.WatchDownload(page, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := monitor.WatchDownload(page, file); err == nil {
		t.Fatal("自动回复和后台监听器各自登记了同一份附件")
	}
	stop()
	_, stop, err = monitor.WatchDownload(page, file)
	if err != nil {
		t.Fatal("取消订阅没有释放后台登记", err)
	}
	stop()
}
