// Package auto_reply 本文件在条件满足后启动可恢复的本地推荐任务，不阻塞消息发送。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"sort"
	"time"
)

// generateRecommendation 在本地复核全部必要条件并生成报告，按输入摘要去重且最多尝试三次。
func (r *AIResponder) generateRecommendation(input ReplyContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if !r.Cloud.LocalAutoReplyEnabled() || input.CandidateState.Candidate == nil {
		return
	}
	items, err := r.Cloud.AutoReplyConfirmationItems(ctx, input.Credentials, input.Conversation.ID)
	if err != nil {
		return
	}
	conditions := []cloud.AutoReplyReportCondition{}
	required := 0
	for _, item := range items {
		if item.ArchivedAt != nil {
			continue
		}
		if item.ItemType == "required" || item.ItemType == "confirm" {
			required++
			if item.Status != "matched" {
				return
			}
		}
		conditions = append(conditions, cloud.AutoReplyReportCondition{ID: item.ID, ItemType: item.ItemType, Content: item.Content, Status: item.Status, StatusReason: item.StatusReason, Evidence: []string{item.EvidenceText}})
	}
	if required == 0 {
		return
	}
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].ID < conditions[j].ID })
	candidate := input.CandidateState.Candidate
	source := cloud.AutoReplyRecommendationReport{
		Candidate: cloud.AutoReplyReportCandidate{ID: candidate.ID, Name: candidate.CandidateName, Gender: candidate.Gender, BirthYM: candidate.BirthYM, Phone: candidate.Phone, Email: candidate.Email, Wechat: candidate.Wechat, WorkRegion: candidate.WorkRegion, WorkYears: candidate.WorkYears, EducationLevel: candidate.EducationLevel, ExpectedPosition: candidate.ExpectedPosition, ExpectedSalaryMin: candidate.ExpectedSalaryMin, ExpectedSalaryMax: candidate.ExpectedSalaryMax, WorkStatus: candidate.WorkStatus, OnlineStatus: candidate.OnlineStatus, PersonalDescription: candidate.PersonalDescription, WorkExperiences: candidate.WorkExperiences, Educations: candidate.Educations, Certificates: candidate.Certificates, Honors: candidate.Honors, ProjectExperiences: candidate.ProjectExperiences},
		Position:  cloud.AutoReplyReportPosition{ID: input.Position.Position.ID, Name: input.Position.Position.Name, PlatformID: input.Position.Position.PlatformID, Description: input.Position.Config.PositionDescription, CompanyName: input.Position.CompanyProfile.Name, Address: input.Position.CompanyProfile.Address, Contact: input.Position.CompanyProfile.Contact, Overview: input.Position.CompanyProfile.Overview, ExtraInfo: input.Position.CompanyProfile.ExtraInfo}, Conditions: conditions,
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return
	}
	digest := sha256.Sum256(encoded)
	key := hex.EncodeToString(digest[:])
	existing, claimed, err := r.Cloud.ClaimAutoReplyReport(ctx, input.Credentials, key)
	if err != nil {
		return
	}
	report := source
	if len(existing) > 0 {
		if json.Unmarshal(existing, &report) != nil {
			return
		}
	} else {
		if !claimed {
			return
		}
		report, err = r.AI.GenerateAutoReplyRecommendation(ctx, input.AIConfig, source)
		if err != nil {
			_ = r.Cloud.SaveAutoReplyReport(context.WithoutCancel(ctx), input.Credentials, key, nil)
			r.report(input, "error", "recommendation", "推荐报告暂时没生成，已保留本地任务供后续重试", false)
			return
		}
		if err = r.Cloud.SaveAutoReplyReport(ctx, input.Credentials, key, &report); err != nil {
			return
		}
	}
	if input.Position.Position.OutputStructuredResume {
		// 简历同步独立进行；报告在本机已落盘，等待同一份简历关联后再发布，不重复调用AI。
		for attempt := 0; attempt < 15; attempt++ {
			if err = r.Cloud.PublishAutoReplyReport(ctx, input.Credentials, candidate.ID, key, input.AIConfig.Model, report); err == nil {
				break
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
