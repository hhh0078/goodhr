// 文件作用：为统一浏览器冷启动注册本地准备步骤，不让 Worker 负责联网或插件业务。
package client

import (
	"context"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// BeforeBrowserStart 在确认没有运行中浏览器后准备本地资源，并返回本次启动使用的完整请求。
type BeforeBrowserStart func(context.Context, contract.BrowserStartRequest) (contract.BrowserStartRequest, error)

// SetBeforeStart 在依赖组装时注册冷启动准备器，启动锁同时保护状态检查、准备步骤与实际启动。
func (c *Client) SetBeforeStart(prepare BeforeBrowserStart) {
	unlock, _ := c.lockStart(context.Background())
	defer unlock()
	c.beforeStart = prepare
}

// lockStart 可取消地串行启动和停止，避免准备插件期间并发关闭导致状态检查失效。
func (c *Client) lockStart(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.startOnce.Do(func() { c.startMu = make(chan struct{}, 1) })
	select {
	case c.startMu <- struct{}{}:
		return func() { <-c.startMu }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
