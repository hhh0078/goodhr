// Package shared 本文件定义自动回复可选的单会话限制，姓名仅辅助确认，稳定会话与候选人编号必须同时明确。
package shared

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// AutoReplyTargetEnvironment 指定临时进程的强制会话范围，请求不能省略或覆盖该限制。
const AutoReplyTargetEnvironment = "GOODHR_AUTO_REPLY_TARGET_JSON"

// AutoReplyTarget 限定本次自动回复唯一允许处理的候选人，不影响未启用限制的正常任务。
type AutoReplyTarget struct {
	CandidateName       string `json:"candidate_name"`
	PlatformThreadID    string `json:"platform_thread_id"`
	PlatformCandidateID string `json:"platform_candidate_id"`
}

// ResolveAutoReplyTarget 合并进程强制限制与请求限制，普通前端未传目标时仍执行同一不可扩大的范围。
func ResolveAutoReplyTarget(taskType string, requested *AutoReplyTarget) (*AutoReplyTarget, error) {
	if err := ValidateAutoReplyTarget(taskType, requested); err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(os.Getenv(AutoReplyTargetEnvironment))
	if taskType != "auto_reply" || raw == "" {
		return requested, nil
	}
	var forced AutoReplyTarget
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&forced); err != nil {
		return nil, fmt.Errorf("本机单会话限制格式不正确，自动回复没有启动")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("本机单会话限制包含多余内容，自动回复没有启动")
	}
	if err := ValidateAutoReplyTarget(taskType, &forced); err != nil {
		return nil, err
	}
	if requested != nil && *requested != forced {
		return nil, fmt.Errorf("请求与本机指定候选人不一致，不能扩大本次自动回复范围")
	}
	return &forced, nil
}

// ValidateAutoReplyTarget 拒绝空限制和仅靠姓名的范围，防止同名人或缺失编号意外扩大任务范围。
func ValidateAutoReplyTarget(taskType string, target *AutoReplyTarget) error {
	if target == nil {
		return nil
	}
	if taskType != "auto_reply" {
		return fmt.Errorf("单会话限制只能用于自动回复")
	}
	for _, field := range []struct{ name, value string }{
		{"候选人姓名", target.CandidateName}, {"平台会话编号", target.PlatformThreadID}, {"平台候选人编号", target.PlatformCandidateID},
	} {
		if field.value != strings.TrimSpace(field.value) || field.value == "" || len([]rune(field.value)) > 200 {
			return fmt.Errorf("单会话限制需要明确的%s，不能留空或带首尾空格", field.name)
		}
	}
	if strings.HasPrefix(target.PlatformThreadID, "local:") {
		return fmt.Errorf("单会话限制需要平台稳定会话编号，不能使用本机推测编号")
	}
	return nil
}
