// Package cloud 本文件提交已核身的在线基础资料供服务器匹配，并在既有权威候选人实体内缓存结果，不标记为已解析附件。
package cloud

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// AutoReplyOnlineProfileInput 绑定本次岗位、已核对的会话、平台身份和当前可见资料，不接受由本机决定的简历库编号。
type AutoReplyOnlineProfileInput struct {
	PositionID          string                       `json:"position_id"`
	ConversationID      string                       `json:"conversation_id"`
	PlatformID          string                       `json:"platform_id"`
	PlatformCandidateID string                       `json:"platform_candidate_id"`
	PlatformAccountID   string                       `json:"platform_account_id"`
	Profile             model.AutoReplyOnlineProfile `json:"profile"`
}

// AutoReplyOnlineProfileResult 区分简历库编号与自动回复候选人实体，并返回确定匹配、临时资料或冲突状态。
type AutoReplyOnlineProfileResult struct {
	CandidateID string              `json:"candidate_id"`
	MatchStatus string              `json:"match_status"`
	ProfileKey  string              `json:"profile_key"`
	Candidate   StructuredCandidate `json:"candidate"`
}

// SaveAutoReplyOnlineProfile 将读取结果交给服务器唯一匹配，再缓存到同一候选人；禁止在本机根据姓名自行关联。
func (c *Client) SaveAutoReplyOnlineProfile(ctx context.Context, cred AgentCredentials, input AutoReplyOnlineProfileInput) (AutoReplyOnlineProfileResult, error) {
	var result AutoReplyOnlineProfileResult
	if input.ConversationID == "" || input.PositionID == "" || input.PlatformID == "" || input.PlatformCandidateID == "" || input.PlatformAccountID == "" || input.Profile.SourceKey == "" || strings.TrimSpace(input.Profile.RawText) == "" {
		return result, fmt.Errorf("在线资料缺少本轮会话、身份或可核实原文")
	}
	input.Profile.Screenshots = nil
	payload := input
	payload.Profile.Mode = ""
	payload.Profile.Screenshots = nil
	if err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/online-profile", cred.Token, cred.MachineID, payload, &result); err != nil {
		return result, err
	}
	if result.ProfileKey == "" || result.CandidateID == "" && result.MatchStatus != "temporary" {
		return result, fmt.Errorf("服务器尚未确认在线资料保存结果")
	}
	switch result.MatchStatus {
	case "matched", "temporary", "ambiguous", "conflict", "created":
	default:
		return result, fmt.Errorf("服务器在线资料匹配状态不完整")
	}
	return result, c.applyOnlineProfileResult(ctx, cred, input, result)
}

// applyOnlineProfileResult 原子核对原会话与当前候选人，只补空资料且保留附件、发送账本、原本候选人ID和解析状态。
func (c *Client) applyOnlineProfileResult(ctx context.Context, cred AgentCredentials, input AutoReplyOnlineProfileInput, result AutoReplyOnlineProfileResult) error {
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", input.ConversationID)
		if err != nil {
			return err
		}
		if !found || conversation.PlatformID != input.PlatformID || conversation.PlatformAccountID != input.PlatformAccountID || conversation.CandidateID == "" {
			return fmt.Errorf("在线资料与当前会话归属不一致")
		}
		identity, found, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", conversation.PlatformIdentityID)
		if err != nil {
			return err
		}
		if found && identity.PlatformCandidateID != input.PlatformCandidateID {
			return fmt.Errorf("在线资料的平台候选人身份已变化")
		}
		candidate, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", conversation.CandidateID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("本轮在线资料候选人已删除")
		}
		fillOnlineCandidateBasics(&candidate, result.Candidate, input.Profile)
		input.Profile.Screenshots = nil
		candidate.OnlineProfile = &AutoReplyOnlineProfileState{SourceKey: input.Profile.SourceKey, ProfileKey: result.ProfileKey, CandidateID: result.CandidateID, MatchStatus: result.MatchStatus, Profile: input.Profile, UpdatedAt: time.Now().UTC()}
		return tx.Put("candidates", candidate.ID, candidate)
	})
}

// fillOnlineCandidateBasics 仅以服务器匹配结果补齐未知字段，当前在线原文另存BasicInfo，不覆盖已解析的附件原文。
func fillOnlineCandidateBasics(target *AutoReplyStoredCandidate, source StructuredCandidate, profile model.AutoReplyOnlineProfile) {
	for _, pair := range []struct {
		target *string
		value  string
	}{{&target.CandidateName, firstNonBlank(source.CandidateName, profile.CandidateName)}, {&target.Gender, profile.Gender}, {&target.Phone, source.Phone}, {&target.Email, source.Email}, {&target.Wechat, source.Wechat}, {&target.EducationLevel, firstNonBlank(source.EducationLevel, profile.EducationLevel)}, {&target.WorkRegion, source.WorkRegion}, {&target.WorkYears, source.WorkYears}, {&target.ExpectedPosition, source.ExpectedPosition}, {&target.PersonalDescription, source.PersonalDescription}, {&target.WorkStatus, source.WorkStatus}} {
		if *pair.target == "" && strings.TrimSpace(pair.value) != "" {
			*pair.target = pair.value
		}
	}
	if target.ExpectedSalaryMin == nil {
		target.ExpectedSalaryMin = source.ExpectedSalaryMin
	}
	if target.ExpectedSalaryMax == nil {
		target.ExpectedSalaryMax = source.ExpectedSalaryMax
	}
	if len(target.WorkExperiences) == 0 {
		target.WorkExperiences = source.WorkExperiences
	}
	if len(target.Educations) == 0 {
		target.Educations = source.Educations
	}
	target.BasicInfo = profile.RawText
	if !target.ResumeStructured || target.RawText == "" {
		target.RawText = firstNonBlank(source.RawText, profile.RawText)
	}
}

// firstNonBlank 只选择确实存在的字段，不合成姓名、年龄、时间或联系方式。
func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
