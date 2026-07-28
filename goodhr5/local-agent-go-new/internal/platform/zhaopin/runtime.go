// Package zhaopin 组装智联招聘平台适配，页面细节全部由云端强类型选择器配置驱动。
package zhaopin

import "goodhr5/local-agent-go-new/internal/platform/common"

// NewRuntime 创建智联招聘平台运行时。
func NewRuntime() *common.Runtime {
	return common.New("zhaopin")
}
