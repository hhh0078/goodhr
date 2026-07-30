// Package shared 文件作用：在任务上下文中传递“处理完当前对象后停止”的安全停止信号。
package shared

import "context"

type gracefulStopKey struct{}

// WithGracefulStop 把只读安全停止信号放入任务上下文。
func WithGracefulStop(ctx context.Context, signal <-chan struct{}) context.Context {
	return context.WithValue(ctx, gracefulStopKey{}, signal)
}

// GracefulStopSignal 返回任务上下文中的安全停止信号；未配置时返回 nil。
func GracefulStopSignal(ctx context.Context) <-chan struct{} {
	signal, _ := ctx.Value(gracefulStopKey{}).(<-chan struct{})
	return signal
}

// GracefulStopRequested 判断用户是否已经请求在当前对象处理完成后停止。
func GracefulStopRequested(ctx context.Context) bool {
	select {
	case <-GracefulStopSignal(ctx):
		return true
	default:
		return false
	}
}
