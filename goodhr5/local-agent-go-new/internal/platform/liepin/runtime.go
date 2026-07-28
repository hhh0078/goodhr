// Package liepin 组装猎聘企业端平台适配，页面细节全部由云端强类型选择器配置驱动。
package liepin

import "goodhr5/local-agent-go-new/internal/platform/common"

// NewRuntime 创建猎聘企业端平台运行时。
func NewRuntime() *common.Runtime {
	return common.New("liepin")
}
