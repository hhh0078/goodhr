// Package cloud 本文件负责调用云端真实附件简历结构化接口。
package cloud

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// autoReplyResumeStructureTimeout 比云端 AI 四分钟上限多留网络收尾时间。
const autoReplyResumeStructureTimeout = 270 * time.Second

// StructureAutoReplyResume 让云端读取已上传附件并返回严格字段的结构化简历。
func (c *Client) StructureAutoReplyResume(ctx context.Context, credentials AgentCredentials, request AutoReplyResumeStructureRequest) (AutoReplyResumeStructureResult, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return AutoReplyResumeStructureResult{}, err
	}
	if strings.TrimSpace(request.ConversationID) == "" || strings.TrimSpace(request.PositionID) == "" {
		return AutoReplyResumeStructureResult{}, fmt.Errorf("简历结构化缺少会话或岗位")
	}
	if len(request.AttachmentIDs) == 0 {
		return AutoReplyResumeStructureResult{}, fmt.Errorf("简历结构化需要真实附件")
	}
	requestClient := *c
	httpClient := *c.http
	if httpClient.Timeout > 0 && httpClient.Timeout < autoReplyResumeStructureTimeout {
		httpClient.Timeout = autoReplyResumeStructureTimeout
	}
	requestClient.http = &httpClient
	var result AutoReplyResumeStructureResult
	if err := requestClient.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/resume-structure", credentials.Token, credentials.MachineID, request, &result); err != nil {
		return AutoReplyResumeStructureResult{}, err
	}
	if strings.TrimSpace(result.Candidate.CandidateName) == "" && strings.TrimSpace(result.Candidate.Phone) == "" {
		return AutoReplyResumeStructureResult{}, fmt.Errorf("云端没有返回可用的结构化简历")
	}
	return result, nil
}
