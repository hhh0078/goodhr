// Package lifecycle 文件作用：给本地登录绑定和应用生命周期提供筛选补传入口，网络请求在独立循环执行。
package lifecycle

import "context"

// BindScreeningSession 为筛选补传和自动回复待办绑定已验证账号，切换账号取消旧请求。
func (r *Runner) BindScreeningSession(ownerID, token string) {
	if r != nil && r.screenings != nil {
		r.screenings.Bind(ownerID, token)
	}
	if r != nil && r.autoReplyBackground != nil {
		r.autoReplyBackground.Bind(ownerID, token)
	}
}

// RunAutoReplyBackground 独立恢复自动回复通知及报告，不依赖招聘任务正在运行。
func (r *Runner) RunAutoReplyBackground(ctx context.Context) {
	if r != nil && r.autoReplyBackground != nil {
		r.autoReplyBackground.Run(ctx)
	}
}

// RunScreeningSync 在应用上下文内运行补传，登录前无凭证不发送，退出后不再占用网络和数据库。
func (r *Runner) RunScreeningSync(ctx context.Context) {
	if r != nil && r.screenings != nil {
		r.screenings.Run(ctx)
	}
}
