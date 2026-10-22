// 本文件串联完整岗位读取、版本化条件、混合召回、精排和逐人证据核验，明确区分推荐、待核实与部分失败。
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// assistantRecommendationCandidate 绑定实时业务简历与当前版本证据，索引内容本身不能作为可信正文。
type assistantRecommendationCandidate struct {
	Candidate PositionCandidate
	Chunks    []resumeVectorChunk
	Hits      []resumeVectorChunk
	Score     float64
}

// recommendationJob 先读取完整甲方JD；既有平台岗位仅使用已授权列表中的业务描述安全投影。
func (s *AssistantService) recommendationJob(scope CandidateLibraryScope, id string) (HeadhunterJob, string, error) {
	if s.headhunter != nil {
		job, err := s.headhunter.JobByID(scope, id)
		if err == nil {
			return job, "headhunter_job", nil
		}
		if !errors.Is(err, ErrNotFound) {
			return HeadhunterJob{}, "", err
		}
	}
	if s.candidates == nil || s.candidates.positionStore == nil {
		return HeadhunterJob{}, "", ErrNotFound
	}
	positions, err := s.candidates.positionStore.ListPositions(scope.TenantID, scope.UserEmail, true)
	if err != nil {
		return HeadhunterJob{}, "", err
	}
	members, err := s.candidates.tenantStore.ListMembers(scope.TenantID)
	if err != nil {
		return HeadhunterJob{}, "", err
	}
	active := map[string]bool{}
	for _, member := range members {
		if member.Status == "active" {
			active[strings.ToLower(member.Email)] = true
		}
	}
	for _, position := range positions {
		if position.ID == id && active[strings.ToLower(position.UserEmail)] {
			return HeadhunterJob{ID: position.ID, Name: position.Name, Description: position.Description, Requirements: strings.Join(position.Keywords, "、"), Revision: position.UpdatedAt.UnixNano(), Status: "open"}, "platform_position", nil
		}
	}
	return HeadhunterJob{}, "", ErrNotFound
}

// assistantJobSource 组织业务JD和用户显式偏好，绝不包含平台密钥或自动化配置。
func assistantJobSource(job HeadhunterJob, preferences string) string {
	salaryMin, salaryMax := "未填写", "未填写"
	if job.SalaryMin > 0 {
		salaryMin = fmt.Sprintf("%g万元", job.SalaryMin)
	}
	if job.SalaryMax > 0 {
		salaryMax = fmt.Sprintf("%g万元", job.SalaryMax)
	}
	return fmt.Sprintf("岗位：%s\n工作城市：%s\n部门：%s\n岗位职责：%s\n任职要求：%s\n学历：%s\n经验：%s\n最低年薪：%s\n最高年薪：%s\n本次用户偏好：%s", job.Name, firstNonEmpty(job.City, "未填写"), firstNonEmpty(job.Department, "未填写"), job.Description, job.Requirements, firstNonEmpty(job.Education, "未填写"), firstNonEmpty(job.Experience, "未填写"), salaryMin, salaryMax, preferences)
}

// assistantRequirementsHash 固定岗位、需求、模型与评价规则版本，偏好变化后必须生成新检索版本。
func assistantRequirementsHash(job HeadhunterJob, preferences, model string, requirements []assistantRequirement) string {
	raw, _ := json.Marshal(map[string]any{"job_id": job.ID, "job_revision": job.Revision, "source": assistantJobSource(job, preferences), "requirements": requirements, "model": model, "rules": "evidence-four-state-v1", "vector_profile": resumeVectorProfile})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// assistantReliableFilters 将明确必要条件映射为固定预过滤；优先、未知和未穷尽的证书列表永远不用于淘汰。
func assistantReliableFilters(requirements []assistantRequirement, job HeadhunterJob) ([]resumeSearchFilter, error) {
	filters := []resumeSearchFilter{}
	for _, requirement := range requirements {
		if requirement.Filter == nil || requirement.Type != "must_have" || !requirement.Explicit {
			continue
		}
		filter := requirement.Filter
		switch filter.Field {
		case "education_min":
			if filter.Op != "gte" || !strings.Contains(requirement.Source, filter.Value) {
				return nil, errors.New("学历过滤必须来自明确必要条件的原文学历")
			}
			valid := false
			for _, education := range []string{"初中", "高中", "中专", "大专", "本科", "硕士", "博士"} {
				if filter.Value == education {
					valid = true
				}
			}
			if !valid {
				return nil, errors.New("学历过滤只允许标准中文学历")
			}
			filters = append(filters, resumeSearchFilter{Field: filter.Field, Op: filter.Op, Value: filter.Value})
		case "expected_salary_max":
			value, err := strconv.ParseFloat(filter.Value, 64)
			expected := job.SalaryMax * 10 / 12
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || filter.Op != "lte" || job.SalaryMax <= 0 || value <= 0 || value-expected > 0.01 || expected-value > 0.01 || !strings.Contains(requirement.Source, "薪") || !(strings.Contains(requirement.Source, "上限") || strings.Contains(requirement.Source, "最高") || strings.Contains(requirement.Source, "不超过") || strings.Contains(requirement.Source, "以内")) {
				return nil, errors.New("薪资过滤须对应岗位明确最高年薪，单位换算为月K")
			}
			filters = append(filters, resumeSearchFilter{Field: filter.Field, Op: filter.Op, Value: strconv.FormatFloat(expected, 'f', 4, 64)})
		case "certificate":
			continue
		default:
			return nil, errors.New("过滤字段不在后端允许范围")
		}
	}
	return filters, nil
}

// recommendationCurrentCandidates 逐个核对当前权限、行版本及builder原文，去重后最多保留100人进入精排。
func (s *AssistantService) recommendationCurrentCandidates(ctx context.Context, scope CandidateLibraryScope, hits []resumeSearchHit) ([]assistantRecommendationCandidate, int, error) {
	items := []assistantRecommendationCandidate{}
	seen := map[string]bool{}
	stale := 0
	for _, hit := range hits {
		if len(items) >= 100 {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, stale, err
		}
		if hit.CandidateID == "" || seen[hit.CandidateID] {
			continue
		}
		seen[hit.CandidateID] = true
		candidate, err := s.candidates.store.GetPositionCandidate(scope.TenantID, hit.CandidateID, "", scope.UserEmail, scope.IsAdmin)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrRecruitmentAccess) {
				stale++
				continue
			}
			return nil, stale, err
		}
		if candidate.ID == builtinCandidateID || candidate.RowVersion != hit.ResumeVersion {
			stale++
			continue
		}
		if s.vectorStore == nil {
			stale++
			continue
		}
		record, err := s.vectorStore.Get(ctx, scope, candidate.ID)
		if err != nil || !record.Complete || record.Profile != resumeVectorProfile || record.ResumeVersion != candidate.RowVersion || len(record.Chunks) == 0 {
			stale++
			continue
		}
		chunks := buildResumeVectorChunks(candidate)
		current := map[string]string{}
		for _, chunk := range chunks {
			current[chunk.ID] = chunk.Text
		}
		valid := true
		for _, chunk := range record.Chunks {
			if current[chunk.ID] != chunk.Text {
				valid = false
				break
			}
		}
		if !valid {
			stale++
			continue
		}
		items = append(items, assistantRecommendationCandidate{Candidate: candidate, Chunks: chunks, Hits: hit.Evidence, Score: hit.Score})
	}
	return items, stale, nil
}

// evaluateAssistantCandidate 使用当前同一岗位版本独立评估一人，再让独立提示词逐条检查引文是否支持结论。
func (s *AssistantService) evaluateAssistantCandidate(ctx context.Context, scope CandidateLibraryScope, requirements []assistantRequirement, version string, candidate assistantRecommendationCandidate, budget *recommendationBudget) (assistantEvaluation, error) {
	currentScope, err := s.checkRecommendationDocuments(scope, []ResumeRerankDocument{{CandidateID: candidate.Candidate.ID, ResumeVersion: candidate.Candidate.RowVersion}})
	if err != nil {
		return assistantEvaluation{}, err
	}
	scope = currentScope
	chunks, truncated := selectAssistantChunks(candidate.Chunks, requirements, candidate.Hits, 20000)
	var result assistantEvaluation
	input := map[string]any{"candidate_id": candidate.Candidate.ID, "resume_version": candidate.Candidate.RowVersion, "requirements_version": version, "requirements": requirements, "evidence_chunks": chunks, "context_truncated": truncated}
	prompt := `现在进行逐条证据评估。只输出{"candidate_id":"输入编号","resume_version":输入版本,"requirements_version":"输入版本","requirement_checks":[{"requirement_id":"r1","status":"met|partial|not_met|unknown","evidence_refs":["chunk_id"]}],"evidence_refs":[{"requirement_id":"r1","chunk_id":"输入片段ID","path":"输入路径","quote":"逐字摘录的简短原文"}],"strengths":[],"gaps":[],"questions_to_verify":[],"recommendation_status":"稍后由程序决定","truncated":false}。逐条覆盖全部要求。未知或缺失不能写成不符合，优先项不能成为必要条件。非unknown结论必须有支持本条件的逐字引文；not_met也必须有明确矛盾原文，没写不算矛盾。重要经历可以位于后半段。只参与项目不能写成主导；年限按明确时间区间合并去重，没有可靠的技能使用起止时间就标记unknown。岗位能力和地点薪资意愿分别说明，不能以年龄、性别、婚育替代能力。上下文截断处的信息不足只能标未知。优势缺口仅写有证据的结论，不输出姓名。`
	err = s.recommendationJSON(ctx, scope, budget, prompt, input, &result, func() error {
		return validateAssistantEvaluation(&result, candidate.Candidate.ID, candidate.Candidate.RowVersion, version, requirements, chunks)
	})
	if err != nil {
		return result, err
	}
	if scope, err = s.checkRecommendationDocuments(scope, []ResumeRerankDocument{{CandidateID: candidate.Candidate.ID, ResumeVersion: candidate.Candidate.RowVersion}}); err != nil {
		return result, err
	}
	var verification struct {
		Checks []struct {
			RequirementID string `json:"requirement_id"`
			Supported     bool   `json:"supported"`
		} `json:"checks"`
		StrengthsSupported []bool `json:"strengths_supported"`
		GapsSupported      []bool `json:"gaps_supported"`
	}
	err = s.recommendationJSON(ctx, scope, budget, `现在进行独立证据复核。不要因为引用ID存在就认可结论；逐条读原文是否真实支持met、partial或not_met。unknown保持保守。逐条检查优势和缺口是否夸大职责、年限或因果，参与不能变成主导。只返回{"checks":[{"requirement_id":"r1","supported":true}],"strengths_supported":[true],"gaps_supported":[true]}，列表数量对应输入的全部条件、优势和缺口。`, map[string]any{"requirements": requirements, "evidence_chunks": chunks, "proposed_evaluation": result}, &verification, func() error {
		if len(verification.Checks) != len(requirements) || len(verification.StrengthsSupported) != len(result.Strengths) || len(verification.GapsSupported) != len(result.Gaps) {
			return errors.New("证据复核列表数量不一致")
		}
		ids := map[string]bool{}
		for _, requirement := range requirements {
			ids[requirement.ID] = true
		}
		for _, check := range verification.Checks {
			if !ids[check.RequirementID] {
				return errors.New("证据复核条件重复或不存在")
			}
			delete(ids, check.RequirementID)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	supported := map[string]bool{}
	for _, check := range verification.Checks {
		supported[check.RequirementID] = check.Supported
	}
	for index := range result.RequirementChecks {
		check := &result.RequirementChecks[index]
		if !supported[check.RequirementID] {
			check.Status = "unknown"
			check.EvidenceRefs = []string{}
			result.QuestionsToVerify = append(result.QuestionsToVerify, "条件"+check.RequirementID+"的原文支持不足，需要进一步确认")
		}
	}
	refs := []assistantEvidenceRef{}
	for _, ref := range result.EvidenceRefs {
		if supported[ref.RequirementID] {
			refs = append(refs, ref)
		}
	}
	result.EvidenceRefs = refs
	strengths := []string{}
	for index, text := range result.Strengths {
		if verification.StrengthsSupported[index] {
			strengths = append(strengths, text)
		}
	}
	result.Strengths = strengths
	gaps := []string{}
	for index, text := range result.Gaps {
		if verification.GapsSupported[index] {
			gaps = append(gaps, text)
		}
	}
	result.Gaps = gaps
	result.Truncated = truncated
	result.EvidenceVerification = "model_checked_requires_human_quality_acceptance"
	result.rank = candidate.Score
	return result, nil
}

// recommend 根据已查询岗位执行固定推荐流程，emit只发送阶段摘要，结果在最终权限与版本复核后才返回。
func (s *AssistantService) recommend(ctx context.Context, scope CandidateLibraryScope, jobID, preferences string, emit func(string, map[string]any) error) (output map[string]any, runErr error) {
	if s.search == nil || !s.search.Ready() {
		return nil, assistantFatalError{"简历高级搜索尚未配置，请先完成向量索引"}
	}
	if len([]rune(preferences)) > 10000 {
		return nil, errors.New("本次偏好太长，请简化后重试")
	}
	var err error
	if scope, err = s.currentScope(scope); err != nil {
		return nil, err
	}
	progress := func(stage, text string) error {
		if emit == nil {
			return nil
		}
		return emit("phase", map[string]any{"stage": stage, "text": text})
	}
	if err := progress("loading_job", "先核对岗位完整资料，再开始找人"); err != nil {
		return nil, err
	}
	job, jobKind, err := s.recommendationJob(scope, jobID)
	if err != nil {
		return nil, err
	}
	source := assistantJobSource(job, preferences)
	cfg, err := s.builtinForScope(scope)
	if err != nil {
		return nil, err
	}
	// 各模型步骤内部已经最多纠错5次；一旦开始付费流程，外层不得整轮重跑并重复支付已成功或费用未确认的调用。
	defer func() {
		var fatal assistantFatalError
		if runErr != nil && !errors.As(runErr, &fatal) && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
			runErr = assistantFatalError{runErr.Error()}
		}
	}()
	budget := &recommendationBudget{}
	requirements := assistantRequirements{}
	if err = progress("parsing_requirements", "正在区分必要条件、加分项和待确认信息"); err != nil {
		return nil, err
	}
	parsePrompt := `现在进行岗位条件解析。只返回{"requirements":[{"requirement_id":"由程序重写","text":"简短可核验条件","type":"must_have|preferred|unresolved","source":"输入中逐字来源","explicit":true,"filter":{"field":"education_min|expected_salary_max|certificate","op":"gte|lte|eq","value":"标准中文学历或月K数字或证书原文"}}],"questions":[]}。filter可省略；仅明确必要且可可靠标准化的学历下限、薪资上限可过滤。学历用初中/高中/中专/大专/本科/硕士/博士；薪资用候选期望最低月薪K与岗位明确上限比较。证书未填写不等于没有。岗位优先、加分、偏好不能变成必要；原文没有的信息必须unresolved。禁用年龄、性别、婚育、籍贯等条件。JD清楚直接处理，只有关键含糊影响结果才提最多2个问题。最多30条件，每条text最多300字。`
	err = s.recommendationJSON(ctx, scope, budget, parsePrompt, map[string]string{"job_id": job.ID, "job_data": source}, &requirements, func() error {
		if err := validateAssistantRequirements(&requirements, source); err != nil {
			return err
		}
		_, err := assistantReliableFilters(requirements.Requirements, job)
		return err
	})
	if err != nil {
		return nil, err
	}
	version := assistantRequirementsHash(job, preferences, cfg.Model, requirements.Requirements)
	filters, err := assistantReliableFilters(requirements.Requirements, job)
	if err != nil {
		return nil, err
	}
	queryTerms := []string{job.Name}
	for _, requirement := range requirements.Requirements {
		queryTerms = append(queryTerms, requirement.Text)
	}
	queryText := strings.Join(queryTerms, "\n")
	if utf8.RuneCountInString(queryText) > 16000 {
		return nil, errors.New("岗位检索条件超出向量输入上限")
	}
	if err = progress("retrieving", "正在同时检索关键词、职业概况和工作项目经历"); err != nil {
		return nil, err
	}
	vectors, err := s.wallet.EmbedForUser(ctx, scope.UserEmail, []string{queryText})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("岗位向量返回数量不完整")
	}
	var hits []resumeSearchHit
	filterMode := "no_reliable_filter"
	if filtered, ok := s.search.(interface {
		SearchFiltered(context.Context, CandidateLibraryScope, string, []float64, []resumeSearchFilter) ([]resumeSearchHit, error)
	}); ok {
		hits, err = filtered.SearchFiltered(ctx, scope, queryText, vectors[0], filters)
		filterMode = "unknown_preserving_prefilter"
	} else {
		hits, err = s.search.Search(ctx, scope, queryText, vectors[0])
		if len(filters) > 0 {
			filterMode = "post_retrieval_verification"
		}
	}
	if err != nil {
		return nil, err
	}
	if len(hits) > 1000 {
		hits = hits[:1000]
	}
	if scope, err = s.currentScope(scope); err != nil {
		return nil, err
	}
	candidates, stale, err := s.recommendationCurrentCandidates(ctx, scope, hits)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{"retrieved": len(hits), "permission_and_version_valid": len(candidates), "stale_or_inaccessible_skipped": stale, "reranked": 0, "analyzed": 0, "analysis_failed": 0}
	result := map[string]any{"job_id": job.ID, "job_name": job.Name, "job_kind": jobKind, "requirements_version": version, "requirements": requirements.Requirements, "clarifying_questions": requirements.Questions, "recommendations": []assistantEvaluation{}, "pending_verification": []assistantEvaluation{}, "stage_counts": counts, "filter_mode": filterMode, "filter_count": len(filters), "evaluation_rules": "evidence-four-state-v1", "ranking_is_hiring_probability": false, "partial_failure": false}
	if len(candidates) == 0 {
		if err = s.checkRecommendationJob(scope, job, preferences); err != nil {
			return nil, err
		}
		result["message"] = "当前没有检索到已完成索引且仍有权限的候选人；未知条件没有当成不符合。"
		result["usage_budget"] = budget
		return result, nil
	}
	if err = progress("reranking", fmt.Sprintf("已找到%d位当前可访问候选人，正在按真实经历精排", len(candidates))); err != nil {
		return nil, err
	}
	documents := []ResumeRerankDocument{}
	for _, candidate := range candidates {
		chunks, _ := selectAssistantChunks(candidate.Chunks, requirements.Requirements, candidate.Hits, 6000)
		raw, _ := json.Marshal(chunks)
		documents = append(documents, ResumeRerankDocument{CandidateID: candidate.Candidate.ID, ResumeVersion: candidate.Candidate.RowVersion, Text: string(raw)})
	}
	scores, mode, err := s.rerankAssistantDocuments(ctx, scope, requirements.Requirements, documents, budget)
	if err != nil {
		return nil, err
	}
	result["reranker"] = mode
	counts["reranked"] = len(scores)
	byID := map[string]float64{}
	for _, score := range scores {
		byID[score.CandidateID] = score.Score
	}
	for index := range candidates {
		candidates[index].Score = byID[candidates[index].Candidate.ID]
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	recommended, pending := []assistantEvaluation{}, []assistantEvaluation{}
	for index, candidate := range candidates[:min(50, len(candidates))] {
		if err = progress("analyzing", fmt.Sprintf("正在核对第%d位候选人的原文证据，已完成%d位", index+1, counts["analyzed"])); err != nil {
			return nil, err
		}
		currentScope, scopeErr := s.currentScope(scope)
		if scopeErr != nil {
			return nil, scopeErr
		}
		scope = currentScope
		current, readErr := s.candidates.store.GetPositionCandidate(scope.TenantID, candidate.Candidate.ID, "", scope.UserEmail, scope.IsAdmin)
		if readErr != nil || current.RowVersion != candidate.Candidate.RowVersion {
			counts["stale_or_inaccessible_skipped"]++
			continue
		}
		item, evaluationErr := s.evaluateAssistantCandidate(ctx, scope, requirements.Requirements, version, candidate, budget)
		if evaluationErr != nil {
			if errors.Is(evaluationErr, errRecommendationBudget) {
				counts["analysis_failed"]++
				result["partial_failure"] = true
				if counts["analyzed"] == 0 {
					return nil, evaluationErr
				}
				break
			}
			var exhausted assistantRecommendationStepError
			if errors.As(evaluationErr, &exhausted) {
				counts["analysis_failed"]++
				result["partial_failure"] = true
				continue
			}
			var fatal assistantFatalError
			if errors.As(evaluationErr, &fatal) || ctx.Err() != nil {
				return nil, evaluationErr
			}
			counts["analysis_failed"]++
			result["partial_failure"] = true
			if errors.Is(evaluationErr, errRecommendationBudget) {
				break
			}
			continue
		}
		counts["analyzed"]++
		item.Name = current.CandidateName
		switch classifyAssistantEvaluation(&item, requirements.Requirements) {
		case "recommended":
			recommended = append(recommended, item)
		case "pending":
			pending = append(pending, item)
		}
	}
	if counts["analyzed"] == 0 && counts["analysis_failed"] > 0 {
		return nil, assistantFatalError{"候选人证据分析未完成，精排结果没有冒充已核实推荐，本轮已停止，请调整条件后重新发起"}
	}
	// 在最终返回之前逐人再次核对当前权限和版本，长任务期间的删除与撤权不能泄露旧结果。
	finalize := func(items []assistantEvaluation) ([]assistantEvaluation, error) {
		valid := []assistantEvaluation{}
		for _, item := range items {
			current, err := s.candidates.store.GetPositionCandidate(scope.TenantID, item.CandidateID, "", scope.UserEmail, scope.IsAdmin)
			if err != nil || current.RowVersion != item.ResumeVersion {
				counts["stale_or_inaccessible_skipped"]++
				continue
			}
			item.Name = current.CandidateName
			valid = append(valid, item)
		}
		sort.SliceStable(valid, func(i, j int) bool {
			return assistantEvaluationPriority(valid[i], requirements.Requirements) > assistantEvaluationPriority(valid[j], requirements.Requirements)
		})
		return valid[:min(10, len(valid))], nil
	}
	if scope, err = s.currentScope(scope); err != nil {
		return nil, err
	}
	if err = s.checkRecommendationJob(scope, job, preferences); err != nil {
		return nil, err
	}
	recommended, err = finalize(recommended)
	if err != nil {
		return nil, err
	}
	pending, err = finalize(pending)
	if err != nil {
		return nil, err
	}
	result["recommendations"] = recommended
	result["pending_verification"] = pending
	result["usage_budget"] = budget
	result["quality_acceptance"] = "结构与原文归属已程序核对，结论另经模型复核；真实招聘质量仍需人工标注集验收"
	if len(recommended) == 0 {
		result["message"] = "暂未找到必要条件都有明确证据的人选；待确认信息已单列，没有擅自放宽必要条件。"
	}
	return result, nil
}

// assistantEvaluationPriority 使用固定偏好与证据覆盖规则排序，不把内部相关度解释为录用概率。
func assistantEvaluationPriority(item assistantEvaluation, requirements []assistantRequirement) float64 {
	preferred := map[string]bool{}
	for _, requirement := range requirements {
		preferred[requirement.ID] = requirement.Type == "preferred"
	}
	points := float64(0)
	for _, check := range item.RequirementChecks {
		if check.Status == "met" {
			points += 2
			if preferred[check.RequirementID] {
				points += 3
			}
		} else if check.Status == "partial" {
			points++
		}
	}
	return points*1000 + item.rank
}
