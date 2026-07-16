// Package positionrunner 文件作用：按职责承载本地岗位运行运行流程的拆分实现。
package positionrunner

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
)

// aiClientForCall 返回本次 AI 调用使用的客户端和清理函数。
// title 和 subtitle 为空时不会画浏览器浮层；找不到浏览器时也不会影响 AI 请求。
func (r *Runner) aiClientForCall(ctx context.Context, exec platformExecutor, client *localai.Client, title string, subtitle string, message string) (*localai.Client, func()) {
	if client == nil {
		return client, func() {}
	}
	title = strings.TrimSpace(title)
	subtitle = strings.TrimSpace(subtitle)
	if title == "" && subtitle == "" {
		return client, func() {}
	}
	if strings.TrimSpace(message) == "" {
		message = "正在等待 AI 返回结果"
	}
	steps := aiThinkingSteps(message)
	overlayCtx, overlayCancel := context.WithTimeout(ctx, overlayActionTimeout)
	_, _ = exec.Post(overlayCtx, "/api/v1/page/ai-overlay", map[string]any{
		"action":   "show",
		"title":    title,
		"subtitle": subtitle,
		"message":  steps[0],
	})
	overlayCancel()
	done := make(chan struct{})
	thinkingCh := make(chan string, 100)
	go r.playAIThinking(ctx, exec, title, subtitle, steps, thinkingCh, done)

	streamingClient := client.WithProgress(func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		select {
		case thinkingCh <- text:
		default:
		}
	})

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			close(done)
			// 不再主动隐藏浮层，由 JS show 端管理旧的卡片 5 秒后自动移除
		})
	}
	return streamingClient, cleanup
}

// showAIReply 在浏览器 AI 浮层里显示本次 AI 的最终回复。
// ctx 为请求上下文，exec 为 Worker 执行器，title、subtitle 和 reply 为展示文本。
func (r *Runner) showAIReply(ctx context.Context, exec platformExecutor, title string, subtitle string, reply string) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = "AI 已完成分析"
	}
	overlayCtx, overlayCancel := context.WithTimeout(context.WithoutCancel(ctx), overlayActionTimeout)
	_, _ = exec.Post(overlayCtx, "/api/v1/page/ai-overlay", map[string]any{
		"action":   "show",
		"title":    title,
		"subtitle": subtitle,
		"message":  reply,
	})
	overlayCancel()
}

// showKeywordMatchOverlay 在浏览器浮层中展示 OCR 关键词匹配结果。
// ctx 为请求上下文，exec 为 Worker 执行器，position 为岗位运行记录，candidate 为候选人。
func (r *Runner) showKeywordMatchOverlay(ctx context.Context, exec platformExecutor, position localdb.Position, candidate map[string]any) {
	state := buildKeywordMatchState(position, candidate)
	overlayCtx, overlayCancel := context.WithTimeout(context.WithoutCancel(ctx), overlayActionTimeout)
	_, _ = exec.Post(overlayCtx, "/api/v1/page/keyword-overlay", map[string]any{
		"action":           "show",
		"title":            "关键词匹配",
		"subtitle":         candidateLogName(candidate),
		"keywords":         state.Keywords,
		"exclude_keywords": state.Excludes,
		"matched_keywords": state.Matched,
		"matched_excludes": state.Excluded,
		"text":             state.Text,
	})
	overlayCancel()
}

// showKeywordOCRLoadingOverlay 在浏览器浮层中展示 OCR 识别等待状态。
// ctx 为请求上下文，exec 为 Worker 执行器，position 为岗位运行记录，candidate 为候选人。
func (r *Runner) showKeywordOCRLoadingOverlay(ctx context.Context, exec platformExecutor, position localdb.Position, candidate map[string]any) {
	state := buildKeywordMatchState(position, candidate)
	overlayCtx, overlayCancel := context.WithTimeout(context.WithoutCancel(ctx), overlayActionTimeout)
	_, _ = exec.Post(overlayCtx, "/api/v1/page/keyword-overlay", map[string]any{
		"action":           "show",
		"title":            "关键词匹配",
		"subtitle":         candidateLogName(candidate),
		"keywords":         state.Keywords,
		"exclude_keywords": state.Excludes,
		"loading":          true,
		"text":             "OCR图文识别中...",
		"max_age_ms":       30000,
	})
	overlayCancel()
}

// playAIThinking 周期性刷新浏览器里的 AI 思考步骤。
// ctx 为请求上下文，exec 为 Worker 执行器，steps 为要展示的思考过程。
func (r *Runner) playAIThinking(ctx context.Context, exec platformExecutor, title string, subtitle string, steps []string, thinkingCh <-chan string, done <-chan struct{}) {
	if len(steps) == 0 {
		return
	}
	ticker := time.NewTicker(1400 * time.Millisecond)
	defer ticker.Stop()
	index := 1
	streamingStarted := false
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case thinking := <-thinkingCh:
			// 标记已收到流式内容，后续 ticker 不再覆盖
			streamingStarted = true
			overlayCtx, overlayCancel := context.WithTimeout(context.WithoutCancel(ctx), overlayActionTimeout)
			_, _ = exec.Post(overlayCtx, "/api/v1/page/ai-overlay", map[string]any{
				"action":   "show",
				"title":    title,
				"subtitle": subtitle,
				"message":  thinking,
			})
			overlayCancel()
		case <-ticker.C:
			// 收到真实流式内容后不再显示固定步骤
			if streamingStarted {
				continue
			}
			overlayCtx, overlayCancel := context.WithTimeout(context.WithoutCancel(ctx), overlayActionTimeout)
			_, _ = exec.Post(overlayCtx, "/api/v1/page/ai-overlay", map[string]any{
				"action":   "show",
				"title":    title,
				"subtitle": subtitle,
				"message":  steps[index%len(steps)],
			})
			overlayCancel()
			index++
		}
	}
}

// aiThinkingSteps 返回 AI 等待时展示的思考过程。
// seed 为当前 AI 调用的基础说明。
func aiThinkingSteps(seed string) []string {
	base := strings.TrimSpace(seed)
	if base == "" {
		base = "正在分析候选人"
	}
	return []string{
		base,
		"正在读取岗位要求和硬性条件",
		"正在对比候选人经历、学历和技能",
		"正在判断是否达到当前岗位运行阈值",
		"正在整理评分原因和下一步动作",
	}
}

// formatDetailDecisionReply 格式化“是否查看详情”的 AI 回复。
// decision 为 AI 决策结果。
func formatDetailDecisionReply(decision localai.Decision) string {
	action := "不打开详情"
	if decision.ShouldOpenDetail {
		action = "打开详情"
	}
	return fmt.Sprintf("AI 回复：%s\n评分：%.1f / %.1f\n原因：%s", action, decision.Score, decision.Threshold, firstNonEmptyString(decision.Reason, "AI未给出原因"))
}

// formatVisionDecisionReply 格式化详情图片 AI 回复。
// decision 为 AI 决策结果。
func formatVisionDecisionReply(decision localai.Decision) string {
	action := "不打招呼"
	if decision.ShouldGreet {
		action = "建议打招呼"
	}
	detail := strings.TrimSpace(decision.DetailText)
	if len([]rune(detail)) > 80 {
		detail = string([]rune(detail)[:80]) + "..."
	}
	if detail == "" {
		detail = "未返回详情摘要"
	}
	return fmt.Sprintf("AI 回复：%s\n评分：%.1f / %.1f\n原因：%s\n摘要：%s", action, decision.Score, decision.Threshold, firstNonEmptyString(decision.Reason, "AI未给出原因"), detail)
}

// formatGreetCandidateReply 格式化打招呼 AI 回复。
// candidate 为已写入 AI 评分字段的候选人。
func formatGreetCandidateReply(candidate map[string]any) string {
	action := "建议打招呼"
	status := stringFromMap(candidate, "status")
	if status == "skipped" {
		action = "不打招呼"
	}
	return fmt.Sprintf("AI 回复：%s\n评分：%.1f / %.1f\n原因：%s", action, floatFromMap(candidate, "ai_greet_score"), floatFromMap(candidate, "ai_greet_threshold"), firstNonEmptyString(stringFromMap(candidate, "ai_greet_reason"), stringFromMap(candidate, "skip_reason"), "AI未给出原因"))
}

// scoreDetailScreenshotWithClient 使用详情长图一次性完成识别和打招呼评分。
// ctx 为请求上下文，position 为岗位运行记录，candidate 为候选人，screenshot 为拼接后的截图信息，client 为 AI 客户端。
func (r *Runner) scoreDetailScreenshotWithClient(ctx context.Context, position localdb.Position, candidate map[string]any, screenshot map[string]any, client *localai.Client) (localai.Decision, error) {
	if client == nil {
		return localai.Decision{}, fmt.Errorf("AI 客户端未配置")
	}
	filePath := firstNonEmptyString(stringFromMap(screenshot, "file_path"), stringFromMap(screenshot, "path"))
	if filePath == "" {
		return localai.Decision{}, fmt.Errorf("详情截图路径为空")
	}
	imageBytes, err := os.ReadFile(filePath)
	if err != nil {
		return localai.Decision{}, fmt.Errorf("读取详情截图失败：%w", err)
	}
	earlyCh := make(chan localai.Decision, 1)
	finalCh := make(chan pendingAIDecisionResult, 1)
	streamingClient := client.WithEarlyDecision(func(decision localai.Decision) {
		select {
		case earlyCh <- decision:
		default:
		}
	})
	go func() {
		decision, err := streamingClient.ScoreVisionForGreet(ctx, position.PositionSnapshot, candidate, imageBytes)
		finalCh <- pendingAIDecisionResult{Decision: decision, Err: err}
	}()
	select {
	case decision := <-earlyCh:
		candidate[pendingAIVisionDecisionKey] = (<-chan pendingAIDecisionResult)(finalCh)
		r.positionLog(position.ID, "info", fmt.Sprintf("AI图片详情：流式结果已提前解析，候选人=%s，分数=%.1f，原因=%s", candidateLogName(candidate), decision.Score, decision.Reason))
		return decision, nil
	case final := <-finalCh:
		return final.Decision, final.Err
	case <-ctx.Done():
		return localai.Decision{}, ctx.Err()
	}
}

// scoreCandidateForDetail 使用本地 AI 给单个候选人计算看详情评分。
// ctx 为请求上下文，candidate 为候选人，client 为空时会返回配置错误。
func (r *Runner) scoreCandidateForDetail(ctx context.Context, position localdb.Position, candidate map[string]any, client *localai.Client) (localai.Decision, error) {
	status := stringFromMap(candidate, "status")
	if !canContinueCandidate(status) {
		return localai.Decision{Score: 0, Reason: "候选人状态不可继续", ShouldOpenDetail: false}, nil
	}
	if client == nil {
		return localai.Decision{}, fmt.Errorf("AI 客户端未配置")
	}
	decision, err := client.ScoreForDetail(ctx, position.PositionSnapshot, candidate)
	if err != nil {
		r.positionLog(position.ID, "warning", "看详情评分失败："+err.Error())
		return localai.Decision{}, err
	}
	return decision, nil
}

// finalizeCandidateGreetDecision 执行第二次详情分析后的最终打招呼判断。
// ctx 为请求上下文，position 为岗位运行记录，exec 为浏览器执行器，candidate 为候选人，client 为 AI 客户端。
func (r *Runner) finalizeCandidateGreetDecision(ctx context.Context, position localdb.Position, exec platformExecutor, candidate map[string]any, client *localai.Client) (int, error) {
	if !canContinueCandidate(stringFromMap(candidate, "status")) {
		return 0, nil
	}
	if positionMode(position) == "keyword" {
		r.showKeywordMatchOverlay(ctx, exec, position, candidate)
		return r.applyKeywordGreetDecision(position, candidate), nil
	}
	visibleClient, cleanup := r.aiClientForCall(ctx, exec, client, "AI 正在评分", candidateLogName(candidate), "正在根据候选人详情判断是否适合打招呼")
	itemSkipped, err := r.scoreCandidate(ctx, position, candidate, visibleClient)
	cleanup()
	if err == nil {
		r.showAIReply(ctx, exec, "AI 评分完成", candidateLogName(candidate), formatGreetCandidateReply(candidate))
	}
	return itemSkipped, err
}

// applyKeywordGreetDecision 使用云端岗位模板关键词做最终打招呼判断。
// position 为岗位运行记录，candidate 为已补充详情的候选人，返回本次是否跳过。
func (r *Runner) applyKeywordGreetDecision(position localdb.Position, candidate map[string]any) int {
	return applyKeywordGreetDecisionWithLog(position, candidate, func(message string) {
		r.positionLog(position.ID, "info", message)
	})
}

// applyKeywordGreetDecision 使用云端岗位模板关键词做最终打招呼判断。
// position 为岗位运行记录，candidate 为已补充详情的候选人，logf 为空时不写日志。
func applyKeywordGreetDecision(position localdb.Position, candidate map[string]any) int {
	return applyKeywordGreetDecisionWithLog(position, candidate, nil)
}

// buildKeywordMatchState 汇总候选人文本并计算关键词命中情况。
// position 为岗位运行记录，candidate 为候选人。
func buildKeywordMatchState(position localdb.Position, candidate map[string]any) keywordMatchState {
	keywords := stringListFromMap(position.PositionSnapshot, "keywords")
	excludes := stringListFromMap(position.PositionSnapshot, "exclude_keywords")
	text := strings.TrimSpace(strings.Join([]string{
		stringFromMap(candidate, "detail_text"),
		stringFromMap(candidate, "raw_text"),
		stringFromMap(candidate, "ocr_text"),
		stringFromMap(candidate, "ai_vision_text"),
	}, " "))
	lowerText := strings.ToLower(text)
	return keywordMatchState{
		Keywords: keywords,
		Excludes: excludes,
		Matched:  matchedWords(lowerText, keywords),
		Excluded: matchedWords(lowerText, excludes),
		Text:     text,
		AndMode:  boolFromMap(position.PositionSnapshot, "is_and_mode"),
	}
}

// applyKeywordGreetDecision 使用云端岗位模板关键词做最终打招呼判断。
// position 为岗位运行记录，candidate 为已补充详情的候选人，logf 为空时不写日志。
func applyKeywordGreetDecisionWithLog(position localdb.Position, candidate map[string]any, logf func(string)) int {
	state := buildKeywordMatchState(position, candidate)
	if len(state.Excluded) > 0 {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "命中排除词：" + strings.Join(state.Excluded, "、")
		logKeywordDecision(logf, "详情关键词跳过", candidate, "命中排除词="+strings.Join(state.Excluded, "、"))
		return 1
	}
	if len(state.Keywords) > 0 && ((!state.AndMode && len(state.Matched) == 0) || (state.AndMode && len(state.Matched) < len(state.Keywords))) {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = "详情未命中关键词"
		logKeywordDecision(logf, "详情关键词跳过", candidate, fmt.Sprintf("命中=%s 需要=%s", keywordListLabel(state.Matched), keywordListLabel(state.Keywords)))
		return 1
	}
	candidate["status"] = "passed"
	candidate["matched_keywords"] = state.Matched
	logKeywordDecision(logf, "详情关键词通过", candidate, "命中="+keywordListLabel(state.Matched))
	return 0
}

// scoreCandidate 使用本地 AI 给单个候选人评分。
// ctx 为请求上下文，candidate 为候选人，client 为空时会返回配置错误。
func (r *Runner) scoreCandidate(ctx context.Context, position localdb.Position, candidate map[string]any, client *localai.Client) (int, error) {
	status := stringFromMap(candidate, "status")
	if !canContinueCandidate(status) {
		return 0, nil
	}
	if client == nil {
		return 0, fmt.Errorf("AI 客户端未配置")
	}
	candidateName := candidateLogName(candidate)
	r.positionLog(position.ID, "info", fmt.Sprintf("打招呼判断：AI评分开始，候选人=%s，超时=%s", candidateName, aiScoreTimeout.Round(time.Second)))
	var decision localai.Decision
	err := r.withOperationTimeout(ctx, position.ID, candidateName, "AI最终打招呼评分", aiScoreTimeout, func(scoreCtx context.Context) error {
		nextDecision, scoreErr := r.scoreCandidateForGreetWithEarlyReturn(scoreCtx, position, candidate, client)
		decision = nextDecision
		return scoreErr
	})
	if err != nil {
		r.positionLog(position.ID, "warning", fmt.Sprintf("打招呼判断：AI评分失败，候选人=%s，错误=%s", candidateName, err.Error()))
		return 0, err
	}
	candidate["ai_greet_score"] = decision.Score
	candidate["ai_greet_reason"] = decision.Reason
	candidate["ai_greet_threshold"] = decision.Threshold
	candidate["ai_usage"] = decision.Usage
	candidate["ai_elapsed_ms"] = decision.ElapsedMS
	if !decision.ShouldGreet {
		candidate["status"] = "skipped"
		candidate["skip_reason"] = fmt.Sprintf("AI评分低于阈值：%.1f/%.1f，%s", decision.Score, decision.Threshold, decision.Reason)
		r.positionLog(position.ID, "info", fmt.Sprintf("打招呼判断：AI评分完成，候选人=%s，分数=%.1f，阈值=%.1f，是否打招呼=否", candidateName, decision.Score, decision.Threshold))
		return 1, nil
	}
	candidate["status"] = "ai_passed"
	r.positionLog(position.ID, "info", fmt.Sprintf("打招呼判断：AI评分完成，候选人=%s，分数=%.1f，阈值=%.1f，是否打招呼=是", candidateName, decision.Score, decision.Threshold))
	return 0, nil
}

// scoreCandidateForGreetWithEarlyReturn 流式评分时提前返回已完整解析到的 score/reason。
// ctx 为请求上下文，position 为岗位运行记录，candidate 为候选人，client 为 AI 客户端。
func (r *Runner) scoreCandidateForGreetWithEarlyReturn(ctx context.Context, position localdb.Position, candidate map[string]any, client *localai.Client) (localai.Decision, error) {
	type result struct {
		decision localai.Decision
		err      error
	}
	earlyCh := make(chan localai.Decision, 1)
	resultCh := make(chan result, 1)
	streamingClient := client.WithEarlyDecision(func(decision localai.Decision) {
		select {
		case earlyCh <- decision:
		default:
		}
	})
	go func() {
		decision, err := streamingClient.ScoreForGreet(ctx, position.PositionSnapshot, candidate)
		resultCh <- result{decision: decision, err: err}
	}()
	select {
	case decision := <-earlyCh:
		r.positionLog(position.ID, "info", fmt.Sprintf("打招呼判断：AI流式结果已提前解析，候选人=%s，分数=%.1f，原因=%s", candidateLogName(candidate), decision.Score, decision.Reason))
		go func() {
			if final := <-resultCh; final.err != nil {
				r.positionLog(position.ID, "warning", "AI 完整评分输出结束失败："+final.err.Error())
			}
		}()
		return decision, nil
	case final := <-resultCh:
		return final.decision, final.err
	case <-ctx.Done():
		return localai.Decision{}, ctx.Err()
	}
}

// applyKeywordFilter 按岗位运行岗位快照过滤候选人。
// position 为岗位运行记录，candidates 为候选人列表，logf 为空时不写日志。
func applyKeywordFilter(position localdb.Position, candidates []map[string]any, logf func(string)) ([]map[string]any, int) {
	keywords := stringListFromMap(position.PositionSnapshot, "keywords")
	excludes := stringListFromMap(position.PositionSnapshot, "exclude_keywords")
	isAndMode := boolFromMap(position.PositionSnapshot, "is_and_mode")
	if len(keywords) == 0 && len(excludes) == 0 {
		return candidates, 0
	}
	result := []map[string]any{}
	skipped := 0
	for _, candidate := range candidates {
		text := strings.ToLower(stringFromMap(candidate, "raw_text"))
		if matched := matchedWords(text, excludes); len(matched) > 0 {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "命中排除词：" + strings.Join(matched, "、")
			logKeywordDecision(logf, "列表关键词跳过", candidate, "命中排除词="+strings.Join(matched, "、"))
			skipped++
			continue
		}
		matched := matchedWords(text, keywords)
		if len(keywords) > 0 && ((!isAndMode && len(matched) == 0) || (isAndMode && len(matched) < len(keywords))) {
			candidate["status"] = "skipped"
			candidate["skip_reason"] = "未命中关键词"
			logKeywordDecision(logf, "列表关键词跳过", candidate, fmt.Sprintf("命中=%s 需要=%s", keywordListLabel(matched), keywordListLabel(keywords)))
			skipped++
			continue
		}
		candidate["status"] = "passed"
		candidate["matched_keywords"] = matched
		logKeywordDecision(logf, "列表关键词通过", candidate, "命中="+keywordListLabel(matched))
		result = append(result, candidate)
	}
	return result, skipped
}

// logKeywordDecision 写入关键词筛选日志。
// logf 为日志函数，candidate 为候选人，detail 为命中详情。
func logKeywordDecision(logf func(string), prefix string, candidate map[string]any, detail string) {
	if logf == nil {
		return
	}
	logf(fmt.Sprintf("列表过滤：%s，候选人=%s，%s", prefix, candidateLogName(candidate), detail))
}

// keywordListLabel 返回关键词列表日志文案。
// words 为关键词列表，空列表返回“无”。
func keywordListLabel(words []string) string {
	if len(words) == 0 {
		return "无"
	}
	return strings.Join(words, "、")
}

// positionMode 返回岗位运行运行模式。
// position 为岗位运行记录。
func positionMode(position localdb.Position) string {
	mode := strings.ToLower(strings.TrimSpace(position.Mode))
	if mode == "" {
		return "ai"
	}
	return mode
}

// matchedWords 返回命中的关键词列表。
// text 为候选人文本，words 为关键词列表。
func matchedWords(text string, words []string) []string {
	result := []string{}
	for _, word := range words {
		safeWord := strings.ToLower(strings.TrimSpace(word))
		if safeWord != "" && strings.Contains(text, safeWord) {
			result = append(result, word)
		}
	}
	return result
}

// stringListFromMap 从 map 中读取字符串列表。
// item 为原始字典，key 为字段名。
func stringListFromMap(item map[string]any, key string) []string {
	if item == nil {
		return []string{}
	}
	switch value := item[key].(type) {
	case []string:
		return cleanStringList(value)
	case []any:
		result := []string{}
		for _, raw := range value {
			if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return cleanStringList(result)
	case string:
		return splitKeywordText(value)
	default:
		return []string{}
	}
}

// splitKeywordText 拆分关键词文本，兼容中文逗号、英文逗号、顿号、分号、空格和换行。
// text 为原始关键词文本。
func splitKeywordText(text string) []string {
	return cleanStringList(strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || unicode.IsSpace(r)
	}))
}

// cleanStringList 清理字符串数组里的空项和重复项。
// items 为原始字符串数组。
func cleanStringList(items []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}
