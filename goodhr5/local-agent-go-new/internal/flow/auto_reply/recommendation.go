// Package auto_reply 本文件把有效决策形成的推荐保存为本机持久待办，按当前账号恢复并在发布前复核来源版本。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// QueueRecommendation 只在固定流程确认本轮有效且业务提交完成后登记推荐，不在AI工具循环内启动报告。
func (r *AIResponder) QueueRecommendation(ctx context.Context, input ReplyContext) error {
	if r == nil || r.Cloud == nil || !r.Cloud.LocalAutoReplyEnabled() || input.CandidateState.Candidate == nil || normalizeAutoReplyPhone(input.CandidateState.Candidate.Phone) == "" {
		return nil
	}
	if input.CandidateState.Conversation != nil && input.CandidateState.Conversation.ResumeRetry != nil {
		return nil
	}
	items, err := r.Cloud.AutoReplyConfirmationItems(ctx, input.Credentials, input.Conversation.ID)
	if err != nil {
		return err
	}
	source, qualified := recommendationSource(input, items)
	if !qualified {
		return nil
	}
	hash, err := recommendationInputHash(source)
	if err != nil {
		return err
	}
	identity := sha256.Sum256([]byte(input.Conversation.ID + "|" + input.BasedOnMessageKey + "|" + hash + fmt.Sprint(input.Position.Config.Version)))
	return r.Cloud.EnqueueAutoReplyRecommendation(ctx, input.Credentials, cloud.AutoReplyRecommendationJob{
		ID: hex.EncodeToString(identity[:]), InputHash: hash, CandidateID: input.CandidateState.Candidate.ID,
		ConversationID: input.Conversation.ID, Conversation: input.Conversation, BasedOnMessageKey: input.BasedOnMessageKey,
		ConfigVersion: input.Position.Config.Version, Position: input.Position, Candidate: *input.CandidateState.Candidate,
		Source: source, Model: input.AIConfig.Model,
	})
}

// ProcessPendingRecommendations 在已认证账号的后台周期恢复待办；凭证只在内存使用，不依赖候选人发送新消息。
func (r *AIResponder) ProcessPendingRecommendations(ctx context.Context, credentials cloud.AgentCredentials) error {
	if r == nil || r.Cloud == nil || r.AI == nil || !r.Cloud.LocalAutoReplyEnabled() {
		return nil
	}
	if !r.recommendationMu.TryLock() {
		return nil
	}
	defer r.recommendationMu.Unlock()
	jobs, err := r.Cloud.PendingAutoReplyRecommendations(ctx, credentials)
	if err != nil {
		return err
	}
	for index, job := range jobs {
		if index >= 3 {
			break
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		claimed, claimErr := r.Cloud.ClaimAutoReplyRecommendation(ctx, credentials, job.ID)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		completed, processErr := r.processRecommendation(jobCtx, credentials, job)
		cancel()
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		var finishErr error
		if errors.Is(processErr, cloud.ErrAutoReplyReportRetriesExhausted) {
			reason := "报告生成已尝试三次，请人工重试"
			if job.LastError != "" {
				reason = job.LastError + "。" + reason
			}
			finishErr = r.Cloud.BlockAutoReplyRecommendation(finishCtx, credentials, job.ID, reason)
		} else {
			finishErr = r.Cloud.FinishAutoReplyRecommendation(finishCtx, credentials, job.ID, completed, recommendationFailureSummary(processErr))
		}
		finishCancel()
		if finishErr != nil {
			return finishErr
		}
	}
	// 已登记任务先获得执行机会，收据中的慢请求不能每轮挡住已有报告的恢复。
	return r.recoverCommittedRecommendations(ctx, credentials)
}

// recommendationFailureSummary 只记录可信字段名、服务状态码和超时类别，避免模型正文、请求地址和密钥进入待办。
func recommendationFailureSummary(err error) string {
	if err == nil {
		return ""
	}
	var formatErr *ai.RecommendationFormatError
	var serviceErr *ai.ServiceError
	switch {
	case errors.As(err, &formatErr):
		return formatErr.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return "推荐处理超时，请稍后重试"
	case errors.Is(err, context.Canceled):
		return "推荐处理已中断，下次恢复时会继续"
	case errors.As(err, &serviceErr):
		if serviceErr.StatusCode >= 100 && serviceErr.StatusCode <= 599 {
			return fmt.Sprintf("报告模型服务返回 HTTP %d，请检查模型配置后重试", serviceErr.StatusCode)
		}
		return "报告模型连接或响应失败，请稍后重试"
	default:
		return "推荐暂时没完成，请稍后重试"
	}
}

// processRecommendation 复用已生成报告；生成和发布前均复核消息、岗位、公司及条件，过期任务安全结束。
func (r *AIResponder) processRecommendation(ctx context.Context, credentials cloud.AgentCredentials, job cloud.AutoReplyRecommendationJob) (bool, error) {
	valid, err := r.recommendationJobCurrent(ctx, credentials, job)
	if err != nil {
		return false, err
	}
	if !valid {
		return true, nil
	}
	existing, err := r.Cloud.ReadAutoReplyReport(ctx, credentials, job.InputHash)
	if err != nil {
		return false, err
	}
	var config cloud.AIConfig
	claimed := false
	if len(existing) == 0 {
		// 配置接口失败不属于模型生成失败；准备完成后才领取付费调用次数。
		config, err = r.Cloud.EffectiveAI(ctx, credentials.Token)
		if err != nil {
			return false, err
		}
		if err = r.AI.Ready(config); err != nil {
			return false, err
		}
		if err = ctx.Err(); err != nil {
			return false, err
		}
		existing, claimed, err = r.Cloud.ClaimAutoReplyReport(ctx, credentials, job.InputHash)
		if err != nil {
			return false, err
		}
	}
	report := job.Source
	reportSaved := len(existing) > 0
	if claimed && !reportSaved {
		defer func() {
			if !reportSaved {
				_ = r.Cloud.SaveAutoReplyReport(context.WithoutCancel(ctx), credentials, job.InputHash, nil)
			}
		}()
	}
	model := job.Model
	if len(existing) > 0 {
		if err = json.Unmarshal(existing, &report); err != nil {
			return false, err
		}
	} else {
		if !claimed {
			return false, fmt.Errorf("报告任务正在处理中")
		}
		model = config.Model
		billingCtx := shared.WithAIBilling(ctx, r.Logger, "", job.Position.Position.ID, job.Source.Candidate.Name, "AI 推荐报告")
		report, err = r.AI.GenerateAutoReplyRecommendation(billingCtx, config, job.Source)
		if err != nil {
			return false, err
		}
		// 模型返回后新消息可能已改变资格；先检查有效性，再保留可供恢复的报告。
		valid, err = r.recommendationJobCurrent(ctx, credentials, job)
		if err != nil {
			return false, err
		}
		if !valid {
			return true, nil
		}
		if err = r.Cloud.SaveAutoReplyReport(ctx, credentials, job.InputHash, &report); err != nil {
			return false, err
		}
		reportSaved = true
	}
	valid, err = r.recommendationJobCurrent(ctx, credentials, job)
	if err != nil {
		return false, err
	}
	if !valid {
		return true, nil
	}
	if job.Position.Position.OutputStructuredResume {
		if err = r.Cloud.PublishAutoReplyReport(ctx, credentials, job.CandidateID, job.InputHash, model, report); err != nil {
			return false, err
		}
	}
	return true, nil
}

// recommendationJobCurrent 检查本机最新证据与云端授权，任何旧快照都不能继续公开或生成推荐。
func (r *AIResponder) recommendationJobCurrent(ctx context.Context, credentials cloud.AgentCredentials, job cloud.AutoReplyRecommendationJob) (bool, error) {
	snapshot, err := r.Cloud.AutoReplySnapshot(ctx, credentials, job.Position.Position.ID)
	if err != nil {
		return false, err
	}
	oldCompany, _ := json.Marshal(job.Position.CompanyProfile)
	newCompany, _ := json.Marshal(snapshot.CompanyProfile)
	if !snapshot.Config.Enabled || !snapshot.Subscription.Active || !snapshot.Subscription.AllowAutoReply || snapshot.Config.Version != job.ConfigVersion || string(oldCompany) != string(newCompany) || snapshot.Position.OutputStructuredResume != job.Position.Position.OutputStructuredResume {
		return false, nil
	}
	messages, err := r.Cloud.AutoReplyMessages(ctx, credentials, job.ConversationID)
	if err != nil {
		return false, err
	}
	if latestRecommendationCandidateKey(messages) != job.BasedOnMessageKey {
		return false, nil
	}
	items, err := r.Cloud.AutoReplyConfirmationItems(ctx, credentials, job.ConversationID)
	if err != nil {
		return false, err
	}
	state, err := r.Cloud.AutoReplyCandidateState(ctx, credentials, cloud.AutoReplyCandidateLookup{PlatformID: job.Conversation.PlatformID, PlatformAccountID: job.Conversation.PlatformAccountID, PlatformThreadID: job.Conversation.PlatformThreadID})
	if err != nil {
		return false, err
	}
	if state.Candidate == nil || state.Candidate.ID != job.CandidateID || (state.Conversation != nil && state.Conversation.ResumeRetry != nil) {
		return false, nil
	}
	source, qualified := recommendationSource(ReplyContext{Position: snapshot, CandidateState: state, Messages: messages}, items)
	if !qualified {
		return false, nil
	}
	hash, err := recommendationInputHash(source)
	return err == nil && hash == job.InputHash, err
}

// recommendationSource 仅根据有效且有证据的必要条件建立报告源，不允许模型凭空生成候选人和岗位原件。
func recommendationSource(input ReplyContext, items []cloud.CandidateConfirmationItem) (cloud.AutoReplyRecommendationReport, bool) {
	position, candidate := input.Position, input.CandidateState.Candidate
	configured, complete := configuredConfirmations(position, items)
	if candidate == nil || !candidate.ResumeStructured || !complete {
		return cloud.AutoReplyRecommendationReport{}, false
	}
	validation := toolExecutionState{input: input}
	conditions := []cloud.AutoReplyReportCondition{}
	required := 0
	for _, item := range configured {
		if item.ItemType == "required" || item.ItemType == "confirm" {
			required++
			if item.Status != "matched" || validation.validateConfirmationEvidence(item) != nil {
				return cloud.AutoReplyRecommendationReport{}, false
			}
		} else if validation.validateConfirmationEvidence(item) != nil {
			continue
		}
		conditions = append(conditions, cloud.AutoReplyReportCondition{ID: item.ID, ItemType: item.ItemType, Content: item.Content, Status: item.Status, StatusReason: item.StatusReason, Evidence: []string{item.EvidenceText}})
	}
	if required == 0 {
		return cloud.AutoReplyRecommendationReport{}, false
	}
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].ID < conditions[j].ID })
	return cloud.AutoReplyRecommendationReport{
		Candidate: cloud.AutoReplyReportCandidate{ID: candidate.ID, Name: candidate.CandidateName, Gender: candidate.Gender, BirthYM: candidate.BirthYM, Phone: candidate.Phone, Email: candidate.Email, Wechat: candidate.Wechat, WorkRegion: candidate.WorkRegion, WorkYears: candidate.WorkYears, EducationLevel: candidate.EducationLevel, ExpectedPosition: candidate.ExpectedPosition, ExpectedSalaryMin: candidate.ExpectedSalaryMin, ExpectedSalaryMax: candidate.ExpectedSalaryMax, WorkStatus: candidate.WorkStatus, OnlineStatus: candidate.OnlineStatus, PersonalDescription: candidate.PersonalDescription, WorkExperiences: candidate.WorkExperiences, Educations: candidate.Educations, Certificates: candidate.Certificates, Honors: candidate.Honors, ProjectExperiences: candidate.ProjectExperiences},
		Position:  cloud.AutoReplyReportPosition{ID: position.Position.ID, Name: position.Position.Name, PlatformID: position.Position.PlatformID, Description: position.Config.PositionDescription, CompanyName: position.CompanyProfile.Name, Address: position.CompanyProfile.Address, Contact: position.CompanyProfile.Contact, Overview: position.CompanyProfile.Overview, ExtraInfo: position.CompanyProfile.ExtraInfo}, Conditions: conditions,
	}, true
}

// recommendationInputHash 忽略模型对同一证据的解释措辞，防止资格没有变化却反复付费生成报告。
func recommendationInputHash(source cloud.AutoReplyRecommendationReport) (string, error) {
	source.Conditions = append([]cloud.AutoReplyReportCondition(nil), source.Conditions...)
	for index := range source.Conditions {
		source.Conditions[index].StatusReason = ""
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
