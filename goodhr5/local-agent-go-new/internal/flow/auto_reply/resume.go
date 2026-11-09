// Package auto_reply 本文件负责自动回复会话的简历索要、收集、正式入库和附件上传门槛。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

var errAutoReplyResumeDeferred = errors.New("简历暂未处理完成")

// 独立补处理预算覆盖冷历史定位、下载及关闭后的聊天恢复；消息问答已在前轮完成。
const autoReplyResumeCollectTimeout = 90 * time.Second
const autoReplyResumeStructureTimeout = 45 * time.Second

// ensureResume 收集页面已有简历；缺少附件或手机号时保留临时状态，不阻断岗位问答。
// handled 保留旧接口形状，索要简历改为固定流程在回答之后执行。
func (f *Flow) ensureResume(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, basedOnMessageKey string, stats *shared.Stats) (*model.AutoReplyResumeBundle, bool, error) {
	if f.Cloud != nil && f.Cloud.LocalAutoReplyEnabled() && snapshot.ResumeSourceMessageID != "" {
		sourceID, err := f.localResumeSourceID(ctx, prepared, conversation.ID, snapshot.ResumeSourceMessageID)
		if err != nil {
			return nil, false, err
		}
		attachments := []cloud.StoredResumeAttachment{}
		for _, attachment := range state.Attachments {
			if attachment.SourceMessageID == sourceID {
				attachments = append(attachments, attachment)
			}
		}
		state.Attachments, state.HasResumeAttachment = attachments, len(attachments) > 0
	}
	if state.HasResumeAttachment && state.Candidate != nil && state.Candidate.ResumeStructured &&
		(snapshot.ResumeSourceMessageID == "" || state.Candidate.ResumeSourceMessageID == snapshot.ResumeSourceMessageID) {
		if err := f.reuseStoredStructuredResume(ctx, prepared, position, *conversation, snapshot, state, true); err != nil {
			return nil, false, err
		}

		if preservedAutoReplyConversationStatus(conversation.Status) == "waiting_resume" {
			if err := f.saveResumeConversationStatus(ctx, prepared, conversation, "ready"); err != nil {
				stats.Failed++
				return nil, false, err
			}
		}
		return nil, false, nil
	}
	storedResumeText := storedAttachmentText(state.Attachments)
	if !snapshot.ResumeCardAvailable && storedResumeText == "" && !state.HasResumeAttachment {
		// 没有简历只限制正式评估与推荐，候选人的岗位问题仍由 AI 根据配置回答。
		return nil, false, nil
	}

	bundle := model.AutoReplyResumeBundle{
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender,
		OnlineResumeText: storedResumeText, ResumeSourceMessageID: snapshot.ResumeSourceMessageID,
	}
	var err error
	if snapshot.ResumeCardAvailable && len(state.Attachments) == 0 {
		// 附件入口异常不能占住整轮问答；超时只取消本次采集，后续仍使用父上下文核对身份。
		collectAt := time.Now()
		collectDeadline := errors.New("本次简历采集预算已用完")
		collectCtx, cancel := context.WithTimeoutCause(ctx, autoReplyResumeCollectTimeout, collectDeadline)
		// collectFailure 区分采集预算、外层截止与已返回的设备错误；从未收到回执时不猜测下载是否开始。
		collectFailure := func(err error, phase string) error {
			d := failurediag.Detail{Source: "简历附件", Operation: "自动回复收集简历", Phase: phase, Reason: "尚未取得采集结果", Impact: "简历留待补处理，继续岗位问答", ElapsedMS: time.Since(collectAt).Milliseconds(), LimitMS: autoReplyResumeCollectTimeout.Milliseconds()}
			switch {
			case errors.Is(context.Cause(collectCtx), collectDeadline):
				d.Reason = "本次采集预算已用完"
			case errors.Is(ctx.Err(), context.Canceled):
				d.Reason = "上层请求已取消"
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				d.Reason = "上层请求时限已到"
			}
			return failurediag.Wrap(err, d)
		}
		sourceMessageID := strings.TrimSpace(snapshot.ResumeSourceMessageID)
		sourceLoaded := false
		for _, message := range snapshot.Messages {
			if sourceMessageID != "" && sourceMessageID == firstNonEmpty(message.PlatformMessageID, message.Key) {
				sourceLoaded = true
				break
			}
		}
		if sourceMessageID != "" && !sourceLoaded && !prepared.Platform.Behavior.AutoReplyResumeLoadsHistory {
			if _, _, err = runtime.ReadAutoReplyMessages(
				collectCtx, f.Browser, prepared.Platform, snapshot,
				[]string{sourceMessageID}, cloud.AutoReplyMaxHistoryMessages,
			); err != nil {
				err = collectFailure(err, "加载历史简历卡片")
				cancel()
				return nil, false, fmt.Errorf("%w：加载候选人历史简历卡片失败：%w", errAutoReplyResumeDeferred, err)
			}
		}
		bundle, err = runtime.CollectAutoReplyResume(collectCtx, f.Browser, prepared.Platform, snapshot)
		if err != nil {
			err = collectFailure(err, "等待平台采集结果")
		}
		cancel()
		if err != nil {
			return nil, false, fmt.Errorf("%w：读取候选人简历失败：%w", errAutoReplyResumeDeferred, err)
		}
		bundle.ResumeSourceMessageID = firstNonEmpty(bundle.ResumeSourceMessageID, snapshot.ResumeSourceMessageID)
	}
	if strings.TrimSpace(bundle.OnlineResumeText) == "" && len(bundle.AttachmentPaths) == 0 && !state.HasResumeAttachment {
		return nil, false, fmt.Errorf("%w：候选人简历卡片已经出现，但没有读到在线简历或附件", errAutoReplyResumeDeferred)
	}
	attachments, err := f.uploadResumeAttachments(ctx, prepared, *conversation, bundle, state.Attachments)
	if err != nil {
		stats.Failed++
		return nil, false, err
	}
	if len(attachments) == 0 {
		return nil, false, fmt.Errorf("%w：还没取得候选人的简历附件，暂时无法整理简历", errAutoReplyResumeDeferred)
	}
	structured := storedStructuredCandidate(state.Candidate)
	structuredResult := StructuredResume{}
	var structureErr error
	if structurer, ok := f.Responder.(ResumeStructurer); ok {
		startedAt := time.Now()
		structureCtx, cancel := context.WithTimeout(ctx, autoReplyResumeStructureTimeout)
		structuredResult, structureErr = structurer.StructureResume(structureCtx, ResumeStructureContext{
			TaskID: prepared.Request.TaskID, Credentials: credentials(prepared), AIConfig: prepared.Position.AI,
			Position: position, Conversation: *conversation, PageSnapshot: snapshot,
			Resume: bundle, Attachments: attachments, BasedOnMessageKey: basedOnMessageKey,
		})
		cancel()
		if structureErr != nil {
			f.log(prepared.Request.TaskID, "structure_resume", "warning", startedAt, structureErr)
			structuredResult = StructuredResume{}
		}
	} else {
		structureErr = fmt.Errorf("简历整理暂时不可用，请稍后再试")
	}
	mergeStructuredCandidate(&structured, structuredResult.Candidate)
	bundle.CandidateName = firstNonEmpty(structured.CandidateName, bundle.CandidateName, snapshot.CandidateName)
	bundle.Gender = firstNonEmpty(structuredResult.Gender, bundle.Gender, snapshot.Gender, storedCandidateGender(state.Candidate))
	bundle.Phone = firstNonEmpty(normalizeAutoReplyPhone(structured.Phone), normalizeAutoReplyPhone(bundle.Phone))
	bundle.Email = firstNonEmpty(normalizeAutoReplyEmail(structured.Email), normalizeAutoReplyEmail(bundle.Email))
	bundle.Wechat = firstNonEmpty(structured.Wechat, structuredResult.Wechat, bundle.Wechat)
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
	structured.RawText = firstNonEmpty(structuredResult.RawText, bundle.OnlineResumeText)
	if structureErr != nil {
		return nil, false, fmt.Errorf("%w：整理候选人正式简历失败，附件已保留：%w", errAutoReplyResumeDeferred, structureErr)
	}
	if strings.TrimSpace(bundle.Phone) == "" {
		f.log(prepared.Request.TaskID, "resume_waiting_phone", "warning", time.Now(), fmt.Errorf("简历已保留，手机号待确认，先继续回答岗位问题"))
		// 先在服务器保存本次整理的候选人资料，写入简历库前再核对手机号。
	}
	candidateID := ""
	if state.Candidate != nil {
		candidateID = state.Candidate.ID
	}
	input := cloud.AutoReplyCandidateInput{
		StructuredCandidate: structured, LocalCandidateID: candidateID,
		ResumeSourceMessageID: bundle.ResumeSourceMessageID,
		PositionID:            position.Position.ID, PlatformID: prepared.Platform.ID,
		PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID,
		AvatarURL: "",
		Gender:    firstNonEmpty(bundle.Gender, snapshot.Gender), BirthYMPrecision: bundle.BirthYMPrecision,
		BasicInfo: bundle.OnlineResumeText,
	}
	if position.Config.Prompts.Resume != "" {
		if err := f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
			return nil, false, fmt.Errorf("%w：%w", errAutoReplyResumeDeferred, err)
		}
	}
	result, saveErr := f.Cloud.SaveAutoReplyCandidate(ctx, credentials(prepared), input)
	if saveErr != nil {
		stats.Failed++
		return nil, false, fmt.Errorf("保存候选人正式简历失败：%w", saveErr)
	}
	candidateID = strings.TrimSpace(result.CandidateID)
	if candidateID == "" {
		stats.Failed++
		return nil, false, fmt.Errorf("还没取得候选人编号，附件已保留，暂时无法关联简历")
	}
	conversation.CandidateID = candidateID
	if result.PlatformIdentity.ID != "" {
		conversation.PlatformIdentityID = result.PlatformIdentity.ID
	}
	if saveErr = f.saveResumeConversationStatus(ctx, prepared, conversation, "ready"); saveErr != nil {
		stats.Failed++
		return nil, false, saveErr
	}
	if f.serverAnswers != nil {
		f.reportResumeStage(prepared, position, snapshot.CandidateName, "resume_saved", "result", "附件和整理后的资料已保存", false)
	}
	f.queueStructuredCandidateSync(ctx, prepared, position, candidateID, input)
	return &bundle, false, nil
}

// reuseStoredStructuredResume 复用已结构化资料；旧简历仅补原文明示且来源可核验的生日，普通聊天只同步确有补齐的资料。
func (f *Flow) reuseStoredStructuredResume(ctx context.Context, prepared shared.PreparedTask, position cloud.AutoReplyPositionSnapshot, conversation cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, syncExisting bool) error {
	candidate := state.Candidate
	if !f.Cloud.LocalAutoReplyEnabled() || !state.HasResumeAttachment || candidate == nil || !candidate.ResumeStructured || candidate.ID != conversation.CandidateID {
		return nil
	}
	source := candidate.ResumeSourceMessageID
	if snapshot.ResumeSourceMessageID != "" && snapshot.ResumeSourceMessageID != source {
		return nil
	}
	var repaired bool
	if candidate.BirthYM == "" && candidate.BirthYMPrecision == "" && source != "" {
		if month, precision := explicitResumeBirthMonth("", candidate.RawText); month != "" {
			var err error
			repaired, err = f.Cloud.BackfillAutoReplyBirthMonth(ctx, credentials(prepared), conversation.ID, *candidate, month)
			if err != nil {
				return err
			}
			if repaired {
				candidate.BirthYM, candidate.BirthYMPrecision = month, precision
			}
		}
	}
	if syncExisting || repaired {
		input := cloud.AutoReplyCandidateInput{StructuredCandidate: storedStructuredCandidate(candidate), LocalCandidateID: candidate.ID, PositionID: position.Position.ID, PlatformID: prepared.Platform.ID, PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID, Gender: candidate.Gender, BirthYMPrecision: candidate.BirthYMPrecision}
		f.queueStructuredCandidateSync(ctx, prepared, position, candidate.ID, input)
	}
	return nil
}

// saveResumeConversationStatus 在关键简历动作后立即把会话状态写入云端，避免下轮重复索要或永久等待。
func (f *Flow) saveResumeConversationStatus(ctx context.Context, prepared shared.PreparedTask, conversation *cloud.AutoReplyConversation, status string) error {
	if conversation == nil {
		return fmt.Errorf("候选人会话还没准备完整")
	}
	conversation.Status = status
	saved, err := f.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), *conversation)
	if err != nil {
		return fmt.Errorf("更新候选人简历会话状态失败：%w", err)
	}
	if strings.TrimSpace(saved.Status) == "" {
		saved.Status = status
	}
	*conversation = saved
	return nil
}

// uploadResumeAttachments 将附件存入服务器的账号专属目录，供简历解析使用。
func (f *Flow) uploadResumeAttachments(ctx context.Context, prepared shared.PreparedTask, conversation cloud.AutoReplyConversation, bundle model.AutoReplyResumeBundle, existing []cloud.StoredResumeAttachment) ([]cloud.StoredResumeAttachment, error) {
	attachments := append([]cloud.StoredResumeAttachment(nil), existing...)
	sourceID := ""
	if f.Cloud.LocalAutoReplyEnabled() && bundle.ResumeSourceMessageID != "" {
		var err error
		sourceID, err = f.localResumeSourceID(ctx, prepared, conversation.ID, bundle.ResumeSourceMessageID)
		if err != nil {
			return nil, err
		}
	}
	seenPaths := make(map[string]struct{}, len(bundle.AttachmentPaths))
	for _, path := range bundle.AttachmentPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, exists := seenPaths[path]; exists {
			continue
		}
		seenPaths[path] = struct{}{}
		uploadAt := time.Now()
		item, err := f.Cloud.UploadAutoReplyAttachment(ctx, credentials(prepared), cloud.AutoReplyAttachmentUpload{
			FilePath: path, ConversationID: conversation.ID,
			PlatformID: prepared.Platform.ID, ExtractedText: "", SourceMessageID: sourceID,
		})
		if err != nil {
			return nil, failurediag.Wrap(fmt.Errorf("保存候选人简历附件失败：%w", err), failurediag.Detail{Source: "简历附件", Operation: "保存已采集的附件", Phase: "提交附件存储", Reason: "附件未确认入库", ElapsedMS: time.Since(uploadAt).Milliseconds()})
		}
		attachments = append(attachments, item)
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			f.log(prepared.Request.TaskID, "cleanup_uploaded_resume", "warning", time.Now(), fmt.Errorf("简历附件已保存，临时文件清理未完成"))
		}
	}
	return uniqueResumeAttachments(attachments), nil
}

// localResumeSourceID 查找平台简历消息在服务器中的记录编号，供附件关联来源。
func (f *Flow) localResumeSourceID(ctx context.Context, prepared shared.PreparedTask, conversationID, sourceKey string) (string, error) {
	messages, err := f.Cloud.AutoReplyMessages(ctx, credentials(prepared), conversationID)
	if err != nil {
		return "", err
	}
	for _, message := range messages {
		if message.Direction == "candidate" && message.MessageType == "resume" && sourceKey == firstNonEmpty(message.PlatformMessageID, message.Fingerprint) {
			return message.ID, nil
		}
	}
	return "", fmt.Errorf("%w：简历来源消息还没同步完整", errAutoReplyResumeDeferred)
}

// uniqueResumeAttachments 按云端附件编号去重并丢弃没有编号的记录。
func uniqueResumeAttachments(items []cloud.StoredResumeAttachment) []cloud.StoredResumeAttachment {
	result := make([]cloud.StoredResumeAttachment, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, item)
	}
	return result
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

// queueStructuredCandidateSync 在岗位授权后后台同步去原文的结构化资料，重复内容由持久收据去重。
func (f *Flow) queueStructuredCandidateSync(ctx context.Context, prepared shared.PreparedTask, position cloud.AutoReplyPositionSnapshot, candidateID string, input cloud.AutoReplyCandidateInput) {
	if !position.Position.OutputStructuredResume || !f.Cloud.LocalAutoReplyEnabled() {
		return
	}
	if f.serverAnswers != nil {
		if input.Phone == "" {
			f.reportResumeStage(prepared, position, input.CandidateName, "resume_waiting_phone", "result", "资料已保存，确认手机号后可写入简历库", false)
			return
		}
		startedAt := time.Now()
		syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		e := f.Cloud.SyncAutoReplyStructuredCandidate(syncCtx, credentials(prepared), candidateID, input)
		status, statusErr := f.Cloud.CandidatePublicationStatus(syncCtx, credentials(prepared), candidateID, position.Position.ID)
		if e != nil || statusErr != nil || status != "completed" {
			if syncErr := errors.Join(e, statusErr); syncErr != nil {
				f.log(prepared.Request.TaskID, "sync_structured_resume", "warning", startedAt, failurediag.Wrap(syncErr, failurediag.Detail{Source: "后台同步", Operation: "简历库发布", Phase: "提交并核对发布状态", Reason: "未确认同步完成", Impact: "整理结果已保留", LimitMS: 30000, ElapsedMS: time.Since(startedAt).Milliseconds()}))
			}
			f.reportResumeStage(prepared, position, input.CandidateName, "resume_publish_pending", "error", "简历库尚未确认发布。已保留附件和整理结果，稍后重试同步。", false)
			return
		}
		f.reportResumeStage(prepared, position, input.CandidateName, "resume_published", "result", "简历库已确认收到这份简历", true)
		return
	}
	go func() {
		startedAt := time.Now()
		syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := f.Cloud.SyncAutoReplyStructuredCandidate(syncCtx, credentials(prepared), candidateID, input); err != nil {
			safe := errors.New("简历库同步未完成，整理结果已保留")
			details := failurediag.Details(err)
			for i := len(details) - 1; i >= 0; i-- {
				safe = failurediag.Wrap(safe, details[i])
			}
			f.log(prepared.Request.TaskID, "sync_structured_resume", "warning", startedAt, failurediag.Wrap(safe, failurediag.Detail{Source: "后台同步", Operation: "简历库发布", Phase: "提交云端", Reason: "未确认同步完成", Impact: "整理结果已保留", LimitMS: 30000, ElapsedMS: time.Since(startedAt).Milliseconds()}))
		}
	}()
}

// reportResumeStage 将受管附件、字段提取及简历库发布分别记入当前正确候选的进度。
func (f *Flow) reportResumeStage(prepared shared.PreparedTask, position cloud.AutoReplyPositionSnapshot, name, stage, phase, reason string, terminal bool) {
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{Kind: "auto_reply", PositionID: position.Position.ID, CandidateName: name, Stage: stage, Phase: phase, Reason: reason, Terminal: terminal, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
}
