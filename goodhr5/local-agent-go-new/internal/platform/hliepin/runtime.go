// Package hliepin 组装猎聘猎头端平台适配，页面细节全部由云端强类型选择器配置驱动。
package hliepin

import "goodhr5/local-agent-go-new/internal/platform/common"

// NewRuntime 创建猎聘猎头端平台运行时。
func NewRuntime() *common.Runtime {
	return common.New("hliepin")
}
