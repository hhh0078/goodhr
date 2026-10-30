// Package cloud 本文件保留旧云端结构化入口的明确拒绝，防止历史调用方上传简历原件。
package cloud

import (
	"context"
	"fmt"
)

// StructureAutoReplyResume 明确拒绝旧云端AI链路，调用方必须使用本地简历解析器。
func (c *Client) StructureAutoReplyResume(context.Context, AgentCredentials, AutoReplyResumeStructureRequest) (AutoReplyResumeStructureResult, error) {
	return AutoReplyResumeStructureResult{}, fmt.Errorf("简历结构化已改为本地执行，请更新本地自动回复处理器")
}
