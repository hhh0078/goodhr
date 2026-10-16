// 本文件执行已确认附件的服务器后台解析、幂等入库、独立跟进及重启恢复，不依赖插件连接存活。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// StartRecovery 定期接管租约到期的入库断点；模型请求一旦可能发出就绝不自动重发。
func (s *AttachmentImportService) StartRecovery(ctx context.Context) {
	if s.parses.store == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			s.recoverPending()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// recoverPending 只扫描有限持久回执，不扫描用户资料或重新创建任务。
func (s *AttachmentImportService) recoverPending() {
	store, ok := s.parses.store.(attachmentImportPendingStore)
	if !ok {
		return
	}
	ctx, cancel := attachmentImportContext()
	defer cancel()
	records, err := store.PendingImports(ctx)
	if err != nil {
		return
	}
	for _, record := range records {
		s.reconcile(record)
	}
}

// reconcile 对已过期租约执行安全恢复；不确定的 AI 结果只标记，不重复扣费。
func (s *AttachmentImportService) reconcile(record attachmentParseRecord) attachmentParseRecord {
	if !attachmentImportPending(record) || record.LeaseUntil > time.Now().UnixMilli() {
		return record
	}
	if record.Import.CreatedAt > 0 && time.Now().UnixMilli()-record.Import.CreatedAt > attachmentTaskTTL.Milliseconds() {
		s.finish(&record, "uncertain", "SAVE_RECONCILIATION_EXPIRED", "保存回执超过核对期限，请先到简历库确认结果；我没有再次调用 AI", true)
		return record
	}
	if record.Import.Stage == "parsing" {
		s.finish(&record, "uncertain", "AI_RESULT_UNCERTAIN", "完整解析的结果还没确认，可能已计费；我没有再次调用 AI，请先核对任务", true)
		return record
	}
	// queued 尚未发出模型；saving/followups 已持久保存结果，可以用原数据库幂等键继续。
	go s.run(record)
	return record
}

// checkpoint 原子保存下一断点；未确认持久化之前不执行该阶段的外部调用。
func (s *AttachmentImportService) checkpoint(record *attachmentParseRecord) bool {
	version := record.Version
	record.Version++
	ctx, cancel := attachmentImportContext()
	defer cancel()
	ok, err := s.parses.store.Swap(ctx, *record, version)
	return err == nil && ok
}

// run 独立于请求连接执行一次任务，CAS 租约与数据库原有幂等凭据共同防止并发和重启重复保存。
func (s *AttachmentImportService) run(initial attachmentParseRecord) {
	ctx, cancel := attachmentImportContext()
	record, err := s.parses.store.Get(ctx, initial.Owner, initial.Task.ID)
	cancel()
	if err != nil || !attachmentImportPending(record) {
		return
	}
	if record.Task.Status != "queued" && record.LeaseUntil > time.Now().UnixMilli() {
		return
	}
	if record.Import.Stage == "parsing" {
		s.reconcile(record)
		return
	}
	state := record.Import
	if state.Fields == nil && state.Stage != "followups" {
		cfg, code, message := s.userAI(record.Owner)
		if code != "" {
			s.finish(&record, "failed", code, message, false)
			return
		}
		aiCtx, aiCancel := context.WithTimeout(context.Background(), aiRequestTimeout)
		defer aiCancel()
		select {
		case s.parses.slots <- struct{}{}:
		case <-aiCtx.Done():
			s.finish(&record, "failed", "AI_NOT_STARTED", "当前任务还没发出 AI 请求，请稍后再试", false)
			return
		}
		state.Stage, record.Task.Status = "parsing", "running"
		record.LeaseUntil = time.Now().Add(aiRequestTimeout + time.Minute).UnixMilli()
		if !s.checkpoint(&record) {
			<-s.parses.slots
			return
		}
		input := attachmentParseRequest{Phase: "full", RequestKey: state.Input.RequestKey, CaptureID: state.Input.CaptureID, Text: state.Input.Text, TextHash: state.Input.TextHash, Source: state.Input.Source}
		resume, code, message, uncertain := s.parses.parseAI(aiCtx, cfg, input)
		<-s.parses.slots
		if code != "" {
			status := "failed"
			if uncertain {
				status = "uncertain"
			}
			s.finish(&record, status, code, message, true)
			return
		}
		fields, err := attachmentImportFields(resume, state.Input)
		if err != nil {
			s.finish(&record, "failed", "INVALID_PARSED_RESUME", "完整识别的资料还不能保存，请核对人工填写的字段；本次没有再次调用 AI", true)
			return
		}
		state.Fields = fields
	}
	if state.Stage != "followups" {
		state.Stage, record.Task.Status = "saving", "running"
		record.Task.ErrorCode, record.Task.Message = "", "正在保存到简历库，关闭插件也会继续"
		record.LeaseUntil = time.Now().Add(time.Minute).UnixMilli()
		if !s.checkpoint(&record) {
			return
		}
		scope, err := s.currentScope(state.Scope)
		if err != nil {
			s.finish(&record, "failed", "SCOPE_CHANGED", "账号的团队权限已经变化，本次没有继续写入简历", false)
			return
		}
		store, ok := s.candidates.store.(CandidateLibraryStore)
		if !ok {
			s.finish(&record, "failed", "SAVE_UNAVAILABLE", "简历库暂时没准备好，请稍后核对任务", false)
			return
		}
		if state.Input.CandidateID != "" && !state.SaveValidated {
			var current PositionCandidate
			current, err = s.candidates.store.GetPositionCandidate(scope.TenantID, state.Input.CandidateID, "", scope.UserEmail, scope.IsAdmin)
			if err == nil {
				if salaryErr := validateCandidateSalaryPatch(current, state.Fields); salaryErr != nil {
					s.finish(&record, "failed", "INVALID_SALARY_RANGE", "完整解析已调用 AI，但新薪资和已有范围冲突，本次没有入库；请先核对资料，避免重复付费", true)
					return
				}
				// 保存前冻结校验断点；回执未知时优先让原事务幂等查询返回结果，不用后来新值否定已提交操作。
				state.SaveValidated = true
				if !s.checkpoint(&record) {
					return
				}
			}
		}
		var item PositionCandidate
		if err == nil {
			item, err = store.SaveLibraryCandidate(scope, state.Input.CandidateID, state.Fields, state.Input.RowVersion, "attachment-import:"+record.Task.ID)
		}
		if err != nil {
			code := attachmentImportSaveError(err)
			if code != "SAVE_RESULT_UNCERTAIN" {
				s.finish(&record, "failed", code, "完整解析已调用 AI，但本次没有入库；请先核对查重结果、目标版本或收藏夹，避免重复付费", true)
				return
			}
			// 网络中断可能发生在数据库提交之后，保留同一内容和请求键以安全读取原事务回执。
			record.Task.Status, record.Task.ErrorCode, record.Task.Message = "uncertain", code, "保存回执还没确认，我会用原请求编号继续核对，不会再次调用 AI"
			record.LeaseUntil = time.Now().Add(30 * time.Second).UnixMilli()
			s.checkpoint(&record)
			return
		}
		state.CandidateID, state.RowVersion = item.ID, item.RowVersion
		state.Stage = "followups"
		// 主体事务已经提交，临时正文和解析草稿立即移除；独立跟进仍保留冻结内容。
		state.Input.Text, state.Input.AvatarDataURL, state.Fields = "", "", nil
		record.LeaseUntil = time.Now().Add(time.Minute).UnixMilli()
		if !s.checkpoint(&record) {
			return
		}
	} else {
		record.Task.Status = "running"
		record.LeaseUntil = time.Now().Add(time.Minute).UnixMilli()
		if !s.checkpoint(&record) {
			return
		}
	}
	s.followups(&record)
}

// currentScope 重新检查冻结账号的当前团队权限，禁止重启恢复时把意图写到另一个团队。
func (s *AttachmentImportService) currentScope(frozen CandidateLibraryScope) (CandidateLibraryScope, error) {
	tenant, err := s.candidates.tenantStore.GetOrCreateTenant(frozen.UserEmail)
	if err != nil || tenant.ID != frozen.TenantID {
		return frozen, ErrNotFound
	}
	admin, err := s.candidates.tenantStore.IsTenantAdmin(tenant.ID, frozen.UserEmail)
	if err != nil {
		return frozen, err
	}
	frozen.IsAdmin = admin
	return frozen, nil
}

// recommendationPosition 复用岗位可见范围和开启状态，避免用冻结的旧权限越权推荐。
func (s *AttachmentImportService) recommendationPosition(scope CandidateLibraryScope, id string) (Position, error) {
	if s.candidates.positionStore == nil {
		return Position{}, ErrNotFound
	}
	items, err := s.candidates.positionStore.ListPositions(scope.TenantID, scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return Position{}, err
	}
	for _, item := range items {
		if item.ID == id && item.IsOpen {
			return item, nil
		}
	}
	return Position{}, ErrNotFound
}

// followups 用任务派生的独立幂等键保存新建时确认的备注和推荐；失败仅标部分成功，不重跑完整 AI。
func (s *AttachmentImportService) followups(record *attachmentParseRecord) {
	state := record.Import
	scope, scopeErr := s.currentScope(state.Scope)
	if state.Input.Note != nil && !state.NoteDone {
		note := state.Input.Note
		occurred, _ := parseCandidateOccurredAt(note.OccurredAt)
		err := scopeErr
		if err == nil {
			_, err = s.candidates.store.SaveCandidateNote(scope, CandidateNote{CandidateID: state.CandidateID, Content: note.Content, OccurredAt: occurred, IdempotencyKey: "attachment-import:" + record.Task.ID + ":note"})
		}
		if err != nil {
			state.FollowupErrors = append(state.FollowupErrors, "NOTE_SAVE_FAILED")
		}
		state.NoteDone = true
		if !s.checkpoint(record) {
			return
		}
	}
	if state.Input.Recommendation != nil && !state.RecommendationDone {
		recommendation := state.Input.Recommendation
		err := scopeErr
		if err == nil {
			var position Position
			position, err = s.recommendationPosition(scope, recommendation.PositionID)
			if err == nil {
				occurred, _ := parseCandidateOccurredAt(recommendation.OccurredAt)
				store := s.candidates.store.(CandidateLibraryStore)
				_, err = store.SaveCandidateRecommendation(scope, state.CandidateID, position, recommendation.Stage, recommendation.Content, "attachment-import:"+record.Task.ID+":recommendation", occurred)
			}
		}
		if err != nil {
			state.FollowupErrors = append(state.FollowupErrors, "RECOMMENDATION_SAVE_FAILED")
		}
		state.RecommendationDone = true
		if !s.checkpoint(record) {
			return
		}
	}
	if len(state.FollowupErrors) > 0 {
		s.finish(record, "partial_success", "FOLLOWUP_PARTIAL", "简历已入库，部分备注或推荐没保存好；请到简历库核对后补充", false)
		return
	}
	s.finish(record, "completed", "", "简历已保存，辛苦你核对了", false)
}

// finish 持久确认终态并清理临时个人资料，再发送不含候选人信息的站内通知。
func (s *AttachmentImportService) finish(record *attachmentParseRecord, status, code, message string, charged bool) {
	record.Task.Status, record.Task.ErrorCode, record.Task.Message, record.Task.PossiblyCharged = status, code, message, charged
	state := record.Import
	state.Stage = "done"
	state.Input = attachmentImportRequest{RequestKey: record.Task.RequestKey, CaptureID: record.Task.CaptureID, TextHash: record.Task.TextHash}
	state.Fields = nil
	if !s.checkpoint(record) {
		return
	}
	title := "附件简历保存完成"
	if status == "partial_success" {
		title = "附件简历已保存，跟进待补充"
	} else if status != "completed" {
		title = "附件简历保存需要核对"
	}
	if s.messages != nil {
		_, _ = s.messages.Push(record.Owner, MessageInput{Title: title, Body: message, Type: "resume_import", ActionURL: "/admin/resumes", IdempotencyKey: "attachment-import:" + record.Task.ID})
	}
}

// attachmentImportSaveError 将业务冲突与未知提交结果分开，绝不因行版本或重复手机号退回新建。
func attachmentImportSaveError(err error) string {
	switch {
	case errors.Is(err, ErrCandidateDuplicate):
		return "CANDIDATE_DUPLICATE"
	case errors.Is(err, ErrCandidateVersion):
		return "CANDIDATE_VERSION_CONFLICT"
	case errors.Is(err, ErrCandidateRequest):
		return "REQUEST_CONFLICT"
	case errors.Is(err, ErrCandidateFolderInvalid), errors.Is(err, ErrCandidateFolderForbidden), errors.Is(err, ErrCandidateFolderScopeForbidden):
		return "FOLDER_UNAVAILABLE"
	case errors.Is(err, ErrNotFound):
		return "CANDIDATE_NOT_FOUND"
	default:
		return "SAVE_RESULT_UNCERTAIN"
	}
}

// attachmentImportFields 合并服务器完整解析与冻结人工值，仅修改最早的一段履历并保留其余经历。
func attachmentImportFields(parsed map[string]any, input attachmentImportRequest) (map[string]any, error) {
	raw, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	result["candidate_name"], result["phone"], result["gender"] = input.Review.Name, input.Review.Phone, input.Review.Gender
	edited := map[string]bool{}
	for _, id := range input.EditedFields {
		edited[id] = true
	}
	for _, field := range []struct{ ID, Key, Value string }{{"candidate-email", "email", input.Review.Email}, {"city", "work_region", input.Review.Region}, {"position", "current_position", input.Review.Position}, {"expected-position", "expected_position", input.Review.ExpectedPosition}} {
		if edited[field.ID] {
			result[field.Key] = field.Value
		}
	}
	if edited["city"] {
		result["current_region"] = input.Review.Region
	}
	for collection, mapping := range attachmentReviewMappings {
		rows := []map[string]any{}
		if data, err := json.Marshal(result[collection]); err == nil {
			_ = json.Unmarshal(data, &rows)
		}
		index, earliest := -1, "9999"
		for position, row := range rows {
			date := candidateString(row, "start_ym")
			if !attachmentReviewDate.MatchString(date) {
				date = "9999"
			}
			if index == -1 || date < earliest {
				index, earliest = position, date
			}
		}
		keys := [3]string{"company_name", "start_ym", "end_ym"}
		if collection == "educations" {
			keys[0] = "school_name"
		}
		if index < 0 {
			rows = append(rows, map[string]any{})
			index = 0
		}
		for position, id := range mapping {
			rows[index][keys[position]] = input.ReviewFields[id]
		}
		if len(rows) == 1 && len(rows[0]) == 3 && candidateString(rows[0], keys[0]) == "" && candidateString(rows[0], "start_ym") == "" && candidateString(rows[0], "end_ym") == "" {
			rows = []map[string]any{}
		}
		result[collection] = rows
	}
	if birth := candidateString(result, "birth_ym"); len(birth) == 4 && attachmentReviewDate.MatchString(birth) {
		result["basic_info"] = strings.TrimSpace("出生年份：" + birth + "\n" + candidateString(result, "basic_info"))
		result["birth_ym"] = ""
	}
	result["raw_text"], result["source_type"] = input.Text, "browser_extension"
	result["platform_id"], result["source_url"], result["platform_candidate_id"] = input.Source.PlatformID, input.Source.SourceURL, input.Source.PlatformCandidateID
	result["folder_ids"] = input.FolderIDs
	if input.AvatarDataURL != "" {
		result["avatar_data_url"] = input.AvatarDataURL
	}
	if input.Operation == "update" {
		// 沿用插件原有更新规则：未提取到的非人工字段不抹掉已有资料，人工明确清空仍写入。
		for key, value := range result {
			if key == "gender" || key == "folder_ids" || (key == "email" && edited["candidate-email"]) || ((key == "work_region" || key == "current_region") && edited["city"]) || (key == "current_position" && edited["position"]) || (key == "expected_position" && edited["expected-position"]) {
				continue
			}
			if mapping, ok := attachmentReviewMappings[key]; ok {
				if edited[mapping[0]] || edited[mapping[1]] || edited[mapping[2]] {
					continue
				}
			}
			if candidateEmptyValue(value) {
				delete(result, key)
			}
		}
	}
	return candidateMutationFields(result, input.Operation == "create")
}
