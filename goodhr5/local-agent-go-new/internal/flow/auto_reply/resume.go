// Package auto_reply 本文件负责自动回复会话的简历索要、收集、正式入库和附件上传门槛。
package auto_reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ensureResume 在进入 AI 回复前确保已经索要简历，或收集并保存页面已有简历。
// handled 为 true 表示本轮已经发送索要简历话术，不应继续生成第二条回复。
func (f *Flow) ensureResume(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, basedOnMessageKey string, stats *shared.Stats) (*model.AutoReplyResumeBundle, bool, error) {
	if state.HasResumeAttachment && state.Candidate != nil {
		return nil, false, nil
	}
	storedResumeText := storedAttachmentText(state.Attachments)
	if !snapshot.ResumeCardAvailable && storedResumeText == "" && !state.HasResumeAttachment {
		requestMessage := firstNonEmpty(position.Config.ResumeRequestMessage, "你好，能发一份简历吗？")
		unchanged, err := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, snapshot, basedOnMessageKey)
		if err != nil {
			stats.Failed++
			return nil, false, err
		}
		if !unchanged {
			stats.Skipped++
			f.reportChangedMessage(prepared.Request.TaskID, snapshot.CandidateName)
			return nil, true, nil
		}
		duplicate, err := f.replyAlreadyRecorded(ctx, prepared, *conversation, basedOnMessageKey, requestMessage)
		if err != nil {
			stats.Failed++
			return nil, false, err
		}
		if duplicate {
			stats.Skipped++
			return nil, true, nil
		}
		if err = runtime.RequestAutoReplyResume(ctx, f.Browser, prepared.Platform, snapshot); err != nil {
			stats.Failed++
			return nil, false, fmt.Errorf("索要候选人简历失败：%w", err)
		}
		sent, err := f.sendVerifiedMessage(ctx, prepared, runtime, *conversation, snapshot, basedOnMessageKey, requestMessage, false)
		if err != nil {
			stats.Failed++
			return nil, false, err
		}
		if sent {
			stats.Succeeded++
		} else {
			stats.Skipped++
		}
		shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
			Kind: "auto_reply", Phase: "result", Stage: "resume_requested", Terminal: true,
			CandidateName: snapshot.CandidateName, Accepted: boolPointer(sent),
			Reason: "还缺一份简历，我已经礼貌问候了", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		return nil, true, nil
	}

	bundle := model.AutoReplyResumeBundle{
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender,
		OnlineResumeText: storedResumeText,
	}
	var err error
	if snapshot.ResumeCardAvailable {
		bundle, err = runtime.CollectAutoReplyResume(ctx, f.Browser, prepared.Platform, snapshot)
		if err != nil {
			stats.Failed++
			return nil, false, fmt.Errorf("读取候选人简历失败：%w", err)
		}
	}
	if strings.TrimSpace(bundle.OnlineResumeText) == "" && len(bundle.AttachmentPaths) == 0 && !state.HasResumeAttachment {
		stats.Failed++
		return nil, false, fmt.Errorf("候选人简历卡片已经出现，但没有读到在线简历或附件")
	}
	structured := storedStructuredCandidate(state.Candidate)
	structuredResult := StructuredResume{}
	var structureErr error
	if structurer, ok := f.Responder.(ResumeStructurer); ok && strings.TrimSpace(bundle.OnlineResumeText) != "" {
		startedAt := time.Now()
		structuredResult, structureErr = structurer.StructureResume(ctx, ResumeStructureContext{
			TaskID: prepared.Request.TaskID, Credentials: credentials(prepared),
			AIConfig: prepared.Position.AI, EnableThinking: prepared.Position.EnableThinking,
			Position: position, Conversation: *conversation, PageSnapshot: snapshot,
			Resume: bundle, BasedOnMessageKey: basedOnMessageKey,
		})
		if structureErr != nil {
			f.log(prepared.Request.TaskID, "structure_resume", "warning", startedAt, structureErr)
			structuredResult = StructuredResume{}
		}
	}
	mergeStructuredCandidate(&structured, structuredResult.Candidate)
	bundle.CandidateName = firstNonEmpty(snapshot.CandidateName, bundle.CandidateName, structured.CandidateName)
	bundle.Gender = firstNonEmpty(snapshot.Gender, bundle.Gender, structuredResult.Gender, storedCandidateGender(state.Candidate))
	bundle.Phone = firstNonEmpty(normalizeAutoReplyPhone(bundle.Phone), normalizeAutoReplyPhone(structured.Phone))
	bundle.Email = firstNonEmpty(normalizeAutoReplyEmail(bundle.Email), normalizeAutoReplyEmail(structured.Email))
	bundle.Wechat = firstNonEmpty(bundle.Wechat, structuredResult.Wechat)
	if structured.BirthYM != "" && structuredResult.BirthYMPrecision != "" {
		bundle.BirthYM = structured.BirthYM
		bundle.BirthYMPrecision = structuredResult.BirthYMPrecision
	}
	if bundle.BirthYM == "" && state.Candidate != nil {
		bundle.BirthYM = state.Candidate.BirthYM
		bundle.BirthYMPrecision = state.Candidate.BirthYMPrecision
	}
	structured.CandidateName = bundle.CandidateName
	structured.BirthYM = bundle.BirthYM
	structured.Phone = bundle.Phone
	structured.Email = bundle.Email
	structured.Wechat = bundle.Wechat
	structured.RawText = bundle.OnlineResumeText
	if structureErr != nil {
		stats.Failed++
		return nil, false, fmt.Errorf("整理候选人正式简历失败，本地附件已经保留：%w", structureErr)
	}
	if strings.TrimSpace(bundle.Phone) == "" {
		stats.Failed++
		return nil, false, fmt.Errorf("候选人简历里暂时没找到可用手机号，本地附件已经保留，暂时不能入库")
	}
	candidateID := ""
	if state.Candidate != nil {
		candidateID = state.Candidate.ID
	}
	result, saveErr := f.Cloud.SaveAutoReplyCandidate(ctx, credentials(prepared), cloud.AutoReplyCandidateInput{
		StructuredCandidate: structured,
		PositionID:          position.Position.ID, PlatformID: prepared.Platform.ID,
		PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID,
		Gender: firstNonEmpty(bundle.Gender, snapshot.Gender), BirthYMPrecision: bundle.BirthYMPrecision,
		BasicInfo: bundle.OnlineResumeText,
	})
	if saveErr != nil {
		stats.Failed++
		return nil, false, fmt.Errorf("保存候选人正式简历失败：%w", saveErr)
	}
	candidateID = strings.TrimSpace(result.CandidateID)
	if candidateID == "" {
		stats.Failed++
		return nil, false, fmt.Errorf("云端没有返回候选人编号，本地附件已经保留，暂时不能上传")
	}
	conversation.CandidateID = candidateID
	if result.PlatformIdentity.ID != "" {
		conversation.PlatformIdentityID = result.PlatformIdentity.ID
	}
	saved, saveErr := f.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), *conversation)
	if saveErr != nil {
		stats.Failed++
		return nil, false, fmt.Errorf("关联正式简历和候选人会话失败：%w", saveErr)
	}
	*conversation = saved
	for _, path := range bundle.AttachmentPaths {
		if _, err = f.Cloud.UploadAutoReplyAttachment(ctx, credentials(prepared), cloud.AutoReplyAttachmentUpload{
			FilePath: path, CandidateID: candidateID, ConversationID: conversation.ID,
			PlatformID: prepared.Platform.ID, ExtractedText: bundle.OnlineResumeText,
		}); err != nil {
			stats.Failed++
			return nil, false, fmt.Errorf("上传候选人简历附件失败：%w", err)
		}
	}
	return &bundle, false, nil
}

// storedAttachmentText 合并云端已有附件的可读正文，供临时候选人下一轮继续结构化。
func storedAttachmentText(attachments []cloud.StoredResumeAttachment) string {
	parts := make([]string, 0, len(attachments))
	seen := make(map[string]struct{}, len(attachments))
	for _, attachment := range attachments {
		text := strings.TrimSpace(attachment.ExtractedText)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

// storedStructuredCandidate 把云端已有简历转换为本轮可增量补齐的结构化候选人。
func storedStructuredCandidate(candidate *cloud.AutoReplyStoredCandidate) cloud.StructuredCandidate {
	if candidate == nil {
		return cloud.StructuredCandidate{}
	}
	return cloud.StructuredCandidate{
		CandidateName: candidate.CandidateName, BirthYM: candidate.BirthYM,
		Phone: candidate.Phone, Email: candidate.Email, Wechat: candidate.Wechat,
		WorkRegion: candidate.WorkRegion,
		WorkYears:  candidate.WorkYears, ExpectedSalaryMin: candidate.ExpectedSalaryMin,
		ExpectedSalaryMax: candidate.ExpectedSalaryMax, EducationLevel: candidate.EducationLevel,
		ExpectedPosition: candidate.ExpectedPosition, OnlineStatus: candidate.OnlineStatus,
		PersonalDescription: candidate.PersonalDescription, WorkStatus: candidate.WorkStatus,
		RawText: candidate.RawText, WorkExperiences: candidate.WorkExperiences,
		Educations: candidate.Educations, Certificates: candidate.Certificates,
		Honors: candidate.Honors, ProjectExperiences: candidate.ProjectExperiences,
		ColleagueCommunications: candidate.Communications,
	}
}

// mergeStructuredCandidate 只用本轮非空 AI 字段补齐简历，避免空数组清掉已有资料。
func mergeStructuredCandidate(target *cloud.StructuredCandidate, incoming cloud.StructuredCandidate) {
	if target == nil {
		return
	}
	target.CandidateName = firstNonEmpty(incoming.CandidateName, target.CandidateName)
	target.BirthYM = firstNonEmpty(incoming.BirthYM, target.BirthYM)
	target.Phone = firstNonEmpty(incoming.Phone, target.Phone)
	target.Email = firstNonEmpty(incoming.Email, target.Email)
	target.Wechat = firstNonEmpty(incoming.Wechat, target.Wechat)
	target.WorkRegion = firstNonEmpty(incoming.WorkRegion, target.WorkRegion)
	target.WorkYears = firstNonEmpty(incoming.WorkYears, target.WorkYears)
	if incoming.ExpectedSalaryMin != nil {
		target.ExpectedSalaryMin = incoming.ExpectedSalaryMin
	}
	if incoming.ExpectedSalaryMax != nil {
		target.ExpectedSalaryMax = incoming.ExpectedSalaryMax
	}
	target.EducationLevel = firstNonEmpty(incoming.EducationLevel, target.EducationLevel)
	target.ExpectedPosition = firstNonEmpty(incoming.ExpectedPosition, target.ExpectedPosition)
	target.OnlineStatus = firstNonEmpty(incoming.OnlineStatus, target.OnlineStatus)
	target.PersonalDescription = firstNonEmpty(incoming.PersonalDescription, target.PersonalDescription)
	target.WorkStatus = firstNonEmpty(incoming.WorkStatus, target.WorkStatus)
	target.RawText = firstNonEmpty(incoming.RawText, target.RawText)
	if len(incoming.WorkExperiences) > 0 {
		target.WorkExperiences = incoming.WorkExperiences
	}
	if len(incoming.Educations) > 0 {
		target.Educations = incoming.Educations
	}
	if len(incoming.Certificates) > 0 {
		target.Certificates = incoming.Certificates
	}
	if len(incoming.Honors) > 0 {
		target.Honors = incoming.Honors
	}
	if len(incoming.ProjectExperiences) > 0 {
		target.ProjectExperiences = incoming.ProjectExperiences
	}
	if len(incoming.ColleagueCommunications) > 0 {
		target.ColleagueCommunications = incoming.ColleagueCommunications
	}
}

// storedCandidateGender 返回云端已有候选人的合法性别兜底。
func storedCandidateGender(candidate *cloud.AutoReplyStoredCandidate) string {
	if candidate == nil || (candidate.Gender != "男" && candidate.Gender != "女") {
		return ""
	}
	return candidate.Gender
}

// normalizeAutoReplyPhone 保留可选国际区号，并校验6到20位数字边界。
func normalizeAutoReplyPhone(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "＋", "+"))
	prefix := ""
	if strings.HasPrefix(value, "+") {
		prefix = "+"
	}
	var digits strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
		}
	}
	if digits.Len() < 6 || digits.Len() > 20 {
		return ""
	}
	return prefix + digits.String()
}

// normalizeAutoReplyEmail 只保留结构合理的单个邮箱地址。
func normalizeAutoReplyEmail(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 320 || strings.Count(value, "@") != 1 || strings.HasPrefix(value, "@") || strings.HasSuffix(value, "@") {
		return ""
	}
	return value
}
