// Package lifecycle 本文件将每次 AI 调用的扣费确认写入普通岗位日志，Windows 与两种 Mac 架构共用此实现。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// AIBilling 保存独立费用日志，明确记录回执所属岗位，不受并发预分析或后台尾部响应影响。
func (l *TaskLogger) AIBilling(taskID, positionID, candidateName, stage string, billing ai.Billing) {
	if l == nil {
		return
	}
	message := stage
	if name := strings.TrimSpace(candidateName); name != "" {
		message += "，候选人“" + name + "”"
	}
	status, level := "success", "info"
	if billing.Status != "external" && (billing.Status != "charged" || billing.CostUnits == nil || *billing.CostUnits < 0) {
		status, level = "warning", "warn"
	}
	message += "，" + aiBillingCostMessage(billing)
	message = string(diagnostics.Redact([]byte(message)))
	// 标准日志会在线上传，只输出费用；包含候选人姓名的完整消息仅进入本地岗位日志。
	log.Printf("task_id=%s position_id=%s flow=ai step=ai_billing status=%s message=%q", taskID, positionID, status, "AI 调用，"+aiBillingCostMessage(billing))
	if l.store == nil {
		return
	}
	output, _ := json.Marshal(billing)
	if _, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{
		TaskID: taskID, PositionID: positionID, Flow: "ai", Step: "ai_billing", Status: status, Level: level,
		Category: "运行", Message: message, Output: output,
	}); err != nil {
		log.Printf("task_id=%s flow=ai step=save_billing_log status=warning error=%q", taskID, err)
	}
}

// aiBillingCostMessage 统一生成费用提示，可用于上传时去掉候选人等自动回复本地资料。
func aiBillingCostMessage(billing ai.Billing) string {
	switch {
	case billing.Status == "charged" && billing.CostUnits != nil && *billing.CostUnits >= 0:
		units := *billing.CostUnits
		return fmt.Sprintf("本次消耗 ¥%d.%04d", units/10000, units%10000)
	case billing.Status == "external":
		return "费用由服务商结算，暂无法获取金额"
	default:
		return "本次 AI 金额暂未确认，可稍后核对余额明细"
	}
}
