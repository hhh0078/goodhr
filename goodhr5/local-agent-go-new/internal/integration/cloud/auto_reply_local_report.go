// Package cloud 本文件保存本地推荐任务收据与报告，只有岗位授权后才上传已经生成的结构化报告。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ClaimAutoReplyReport 在本机领取带五分钟租约的报告任务，完成报告直接复用，失败最多尝试三次。
func (c *Client) ClaimAutoReplyReport(ctx context.Context, credentials AgentCredentials, key string) (json.RawMessage, bool, error) {
	if !c.LocalAutoReplyEnabled() {
		return nil, false, fmt.Errorf("本地自动回复存储尚未准备好")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return nil, false, err
	}
	if report, ok := data.Reports[key]; ok {
		return report, false, nil
	}
	if data.ReportAttempts[key] >= 3 || time.Since(data.ReportClaims[key]) < 5*time.Minute {
		return nil, false, nil
	}
	data.ReportAttempts[key]++
	data.ReportClaims[key] = time.Now().UTC()
	return nil, true, c.saveLocalAutoReply(ctx, credentials, data)
}

// SaveAutoReplyReport 保存完整本地报告；失败释放租约，后续轮次可以继续恢复。
func (c *Client) SaveAutoReplyReport(ctx context.Context, credentials AgentCredentials, key string, report *AutoReplyRecommendationReport) error {
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return err
	}
	delete(data.ReportClaims, key)
	if report != nil {
		encoded, encodeErr := json.Marshal(report)
		if encodeErr != nil {
			return encodeErr
		}
		data.Reports[key] = encoded
	}
	return c.saveLocalAutoReply(ctx, credentials, data)
}

// PublishAutoReplyReport 只上传本地已生成且有授权云端简历关联的报告，不请求云端执行AI。
func (c *Client) PublishAutoReplyReport(ctx context.Context, credentials AgentCredentials, localCandidateID, key, model string, report AutoReplyRecommendationReport) error {
	c.localAutoMu.Lock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	c.localAutoMu.Unlock()
	if err != nil {
		return err
	}
	if data.ReportPublished[key] {
		return nil
	}
	cloudID := data.CloudCandidates[localCandidateID]
	if cloudID == "" {
		return fmt.Errorf("结构化简历仍在同步，推荐报告已保存在本机")
	}
	report.Candidate.ID = cloudID
	report.Candidate.BasicInfo = ""
	report.Candidate.AvatarURL = ""
	for index := range report.Conditions {
		report.Conditions[index].Evidence = nil
	}
	payload := struct {
		PositionID  string                        `json:"position_id"`
		CandidateID string                        `json:"candidate_id"`
		InputHash   string                        `json:"input_hash"`
		Report      AutoReplyRecommendationReport `json:"report_data"`
		Model       string                        `json:"model"`
		TokenUsage  int                           `json:"token_usage"`
	}{report.Position.ID, cloudID, key, report, model, 0}
	var result struct {
		OK bool `json:"ok"`
	}
	if err = c.doWithMachineID(ctx, "POST", autoReplyAgentBasePath+"/recommendations/completed", credentials.Token, credentials.MachineID, payload, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("云端尚未确认推荐报告")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err = c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return err
	}
	data.ReportPublished[key] = true
	return c.saveLocalAutoReply(ctx, credentials, data)
}
