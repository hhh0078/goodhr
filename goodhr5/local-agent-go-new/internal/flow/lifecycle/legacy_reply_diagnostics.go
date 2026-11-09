// Package lifecycle 本文件从旧自动回复岗位日志生成不含姓名、原话、工具参数或附件路径的技术摘要。
package lifecycle

import (
	"encoding/json"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/storage"
	"regexp"
)

var legacyReplyTechnicalKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,95}$`)
var legacyReplyErrorCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,95}$`)
var legacyBillingModel = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,55}(/[A-Za-z0-9][A-Za-z0-9_.-]{0,47})?(:[A-Za-z0-9_-]{1,16})?$`)

// legacyTechnicalValue 只保留简短的技术标识，危险或带候选人原文的旧值改用固定标签。
func legacyTechnicalValue(value, fallback string) string {
	if legacyReplyTechnicalKey.MatchString(value) {
		return value
	}
	return fallback
}

// legacyCodeOnly 从浏览器输入或结果读取稳定错误码，丢弃其中的页面、命令参数和异常详情。
func legacyCodeOnly(raw json.RawMessage) json.RawMessage {
	var v struct {
		Code string `json:"error_code"`
	}
	if json.Unmarshal(raw, &v) != nil || !legacyReplyErrorCode.MatchString(v.Code) {
		return json.RawMessage("{}")
	}
	safe, _ := json.Marshal(v)
	return safe
}

// RedactLegacyReplyLog 复用现有步骤摘要与计费展示，第二个返回值标识是否确有可核验的费用回执。
func RedactLegacyReplyLog(item storage.TaskLog) (storage.TaskLog, bool) {
	var billing ai.Billing
	valid := false
	if item.Flow == "ai" && item.Step == "ai_billing" && json.Unmarshal(item.Output, &billing) == nil {
		switch billing.Status {
		case "charged":
			valid = billing.CostUnits != nil && *billing.CostUnits >= 0
		case "unavailable", "failed", "external":
			billing.CostUnits = nil
			valid = true
		}
	}
	item.Flow = legacyTechnicalValue(item.Flow, "auto_reply")
	item.Step = legacyTechnicalValue(item.Step, "legacy_step")
	item.Status = legacyTechnicalValue(item.Status, "unknown")
	item.Level = legacyTechnicalValue(item.Level, "info")
	switch item.Category {
	case "运行", "错误", "动作", "诊断", "AI":
	default:
		item.Category = "运行"
	}
	item.ErrorMessage = ""
	item.Input, item.Output = legacyCodeOnly(item.Input), legacyCodeOnly(item.Output)
	item.Message = taskLogMessage(item.Step, item.Status, nil)
	if valid {
		if !legacyBillingModel.MatchString(billing.Model) {
			billing.Model = ""
		}
		item.Output, _ = json.Marshal(billing)
		item.Message = "AI 调用，" + aiBillingCostMessage(billing)
	}
	return item, valid
}
