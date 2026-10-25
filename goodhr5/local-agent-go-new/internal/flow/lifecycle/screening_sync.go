// Package lifecycle 文件作用：给本地登录绑定和应用生命周期提供筛选补传入口，网络请求在独立循环执行。
package lifecycle

import "context"

// BindScreeningSession 只接收已经通过云端验证的账号和临时凭证；切换账号会取消旧账号在途请求。
func (r *Runner) BindScreeningSession(ownerID, token string) {
	if r != nil && r.screenings != nil {
		r.screenings.Bind(ownerID, token)
	}
}

// RunScreeningSync 在应用上下文内运行补传，登录前无凭证不发送，退出后不再占用网络和数据库。
func (r *Runner) RunScreeningSync(ctx context.Context) {
	if r != nil && r.screenings != nil {
		r.screenings.Run(ctx)
	}
}
