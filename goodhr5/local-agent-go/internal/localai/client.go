// Package localai 负责在本地使用云端下发的 AI 配置调用 OpenAI 兼容接口。
package localai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/localdb"
)

const (
	defaultGreetThreshold  = 70.0
	defaultDetailThreshold = 60.0
	defaultGreetPrompt     = `你是资深招聘顾问。请给候选人打“打招呼建议分”。只输出 JSON：{"score": 78, "reason": "匹配核心要求"}。score 为 0-100 数字，reason 控制在30字以内，禁止 Markdown。`
	defaultDetailPrompt    = `你是资深招聘顾问。请只根据候选人基础信息判断是否值得打开详情。只输出 JSON：{"score": 66, "reason": "可进一步确认细节"}。score 为 0-100 数字，reason 控制在30字以内，禁止 Markdown。`
	defaultVisionSystem    = `你是资深招聘顾问。请先识别图片中的候选人详情，再结合岗位要求完成打招呼评分。只输出 JSON，禁止 Markdown。JSON 必须可直接入库，字段固定为：candidate_name、birth_ym、phone、email、work_region、work_years、expected_salary_min、expected_salary_max、education_level、expected_position、online_status、personal_description、work_status、work_experiences、educations、certificates、honors、project_experiences、colleague_communications、ai、raw_text。ai.detail 和 ai.greet 均包含 score、reason。经历数组字段格式必须固定：work_experiences 使用 company_name、position_name、content、start_ym、end_ym；educations 使用 school_name、major_name、education_level、start_ym、end_ym；certificates 使用 certificate_name、issued_by、issued_ym；honors 使用 honor_name、issued_by、issued_ym、description；project_experiences 使用 project_name、role_name、content、start_ym、end_ym；colleague_communications 使用 communicator_name、communicated_at、content。没有的信息用空字符串、null 或空数组。`
)

// Client 是本地 AI 调用客户端。
type Client struct {
	Config         localdb.AIConfig
	HTTPClient     *http.Client
	Progress       func(string)
	EarlyDecision  func(Decision)
	EnableThinking bool // 开启后显示 reasoning_content 流式思考
}

// Decision 表示 AI 评分结果。
type Decision struct {
	Score            float64        `json:"score"`
	Reason           string         `json:"reason"`
	DetailText       string         `json:"detail_text"`
	ShouldGreet      bool           `json:"should_greet"`
	ShouldOpenDetail bool           `json:"should_open_detail"`
	Threshold        float64        `json:"threshold"`
	Usage            map[string]any `json:"usage"`
	ElapsedMS        int            `json:"elapsed_ms"`
	// ResumeData 保存 AI 返回的结构化简历 JSON 原始内容（新格式 resume 字段），
	// 由 parseVisionScoreJSON 填充，上游可存入数据库。
	ResumeData map[string]any `json:"resume_data"`
}

// ChatResult 表示本地 AI 通用聊天结果。
type ChatResult struct {
	Content   string         `json:"content"`
	Usage     map[string]any `json:"usage"`
	ElapsedMS int            `json:"elapsed_ms"`
}

// New 创建本地 AI 客户端。
// config 为本地保存的 AI 配置。
func New(config localdb.AIConfig) *Client {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	return &Client{
		Config: config,
		HTTPClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

// WithProgress 返回带流式进度回调的 AI 客户端副本。
// progress 为流式内容更新回调，为空时保持普通非流式请求。
func (c *Client) WithProgress(progress func(string)) *Client {
	if c == nil {
		return nil
	}
	clone := *c
	clone.Progress = progress
	return &clone
}

// WithEarlyDecision 返回带提前评分回调的 AI 客户端副本。
// earlyDecision 为流式文本中提前解析到评分 JSON 后的回调。
func (c *Client) WithEarlyDecision(earlyDecision func(Decision)) *Client {
	if c == nil {
		return nil
	}
	clone := *c
	clone.EarlyDecision = earlyDecision
	return &clone
}

// withDecisionThreshold 为提前评分结果补充阈值和动作判断。
// threshold 为评分阈值，isGreet 表示是否为打招呼评分。
func (c *Client) withDecisionThreshold(threshold float64, isGreet bool) *Client {
	if c == nil || c.EarlyDecision == nil {
		return c
	}
	clone := *c
	origin := c.EarlyDecision
	clone.EarlyDecision = func(decision Decision) {
		decision.Score = clampScore(decision.Score)
		decision.Reason = truncate(decision.Reason, 30)
		if decision.Reason == "" {
			decision.Reason = "AI未给出原因"
		}
		decision.Threshold = threshold
		if isGreet {
			decision.ShouldGreet = decision.Score >= threshold
		} else {
			decision.ShouldOpenDetail = decision.Score >= threshold
		}
		origin(decision)
	}
	return &clone
}

// ScoreForDetail 给候选人计算查看详情评分。
// ctx 为请求上下文，position 为岗位快照，candidate 为候选人基础信息。
func (c *Client) ScoreForDetail(ctx context.Context, position map[string]any, candidate map[string]any) (Decision, error) {
	threshold := numberFromAIConfig(position, defaultDetailThreshold, "detail_score_threshold", "open_detail_threshold", "detail_threshold")
	result, err := c.chatMessages(ctx, buildDetailMessages(position, candidate), numberFromAIConfig(position, c.Config.Temperature, "temperature"))
	if err != nil {
		return Decision{}, err
	}
	score, reason, err := parseScoreJSON(result.Content)
	if err != nil {
		return Decision{}, err
	}
	score = clampScore(score)
	reason = truncate(reason, 30)
	if reason == "" {
		reason = "AI未给出原因"
	}
	return Decision{
		Score:            score,
		Reason:           reason,
		ShouldOpenDetail: score >= threshold,
		Threshold:        threshold,
		Usage:            result.Usage,
		ElapsedMS:        result.ElapsedMS,
	}, nil
}

// ScoreForGreet 给候选人计算打招呼评分。
// ctx 为请求上下文，position 为岗位快照，candidate 为候选人信息。
func (c *Client) ScoreForGreet(ctx context.Context, position map[string]any, candidate map[string]any) (Decision, error) {
	threshold := numberFromAIConfig(position, defaultGreetThreshold, "greet_score_threshold", "greet_threshold")
	c = c.withDecisionThreshold(threshold, true)
	result, err := c.chatMessages(ctx, buildGreetMessages(position, candidate), numberFromAIConfig(position, c.Config.Temperature, "temperature"))
	if err != nil {
		return Decision{}, err
	}
	score, reason, err := parseScoreJSON(result.Content)
	if err != nil {
		return Decision{}, err
	}
	score = clampScore(score)
	reason = truncate(reason, 30)
	if reason == "" {
		reason = "AI未给出原因"
	}
	return Decision{
		Score:       score,
		Reason:      reason,
		ShouldGreet: score >= threshold,
		Threshold:   threshold,
		Usage:       result.Usage,
		ElapsedMS:   result.ElapsedMS,
	}, nil
}

// ScoreVisionForGreet 根据候选人详情长图一次性完成详情识别和打招呼评分。
// ctx 为请求上下文，position 为岗位快照，candidate 为候选人信息，imageBytes 为拼接后的详情截图。
func (c *Client) ScoreVisionForGreet(ctx context.Context, position map[string]any, candidate map[string]any, imageBytes []byte) (Decision, error) {
	threshold := numberFromAIConfig(position, defaultGreetThreshold, "greet_score_threshold", "greet_threshold")
	c = c.withDecisionThreshold(threshold, true)
	userText := buildVisionUserPrompt(position, candidate)
	content := []map[string]any{
		{"type": "text", "text": userText},
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)}},
	}
	result, err := c.Chat(ctx, map[string]any{
		"messages":        []map[string]any{{"role": "system", "content": buildVisionSystemPrompt(position)}, {"role": "user", "content": content}},
		"temperature":     0.1,
		"enable_thinking": c.EnableThinking,
	})
	if err != nil {
		return Decision{}, err
	}
	score, reason, detailText, resumeData, err := parseVisionScoreJSONWithResume(result.Content)
	if err != nil {
		return Decision{}, err
	}
	score = clampScore(score)
	reason = truncate(reason, 30)
	if reason == "" {
		reason = "AI未给出原因"
	}
	return Decision{
		Score:       score,
		Reason:      reason,
		DetailText:  strings.TrimSpace(detailText),
		ShouldGreet: score >= threshold,
		Threshold:   threshold,
		Usage:       result.Usage,
		ElapsedMS:   result.ElapsedMS,
		ResumeData:  resumeData,
	}, nil
}

// Chat 调用本地 AI 通用聊天接口。
// ctx 为请求上下文，payload 为 OpenAI 兼容聊天参数。
func (c *Client) Chat(ctx context.Context, payload map[string]any) (ChatResult, error) {
	apiURL := chatCompletionsURL(c.Config.BaseURL)
	if apiURL == "" {
		return ChatResult{}, fmt.Errorf("请先在个人配置里填写本地 AI 接口地址")
	}
	if strings.TrimSpace(c.Config.APIKey) == "" {
		return ChatResult{}, fmt.Errorf("请先在个人配置里填写本地 AI 密钥")
	}
	if strings.TrimSpace(c.Config.Model) == "" {
		return ChatResult{}, fmt.Errorf("请先在个人配置里填写本地 AI 模型名称")
	}
	body := map[string]any{}
	for key, value := range payload {
		body[key] = value
	}
	if _, ok := body["model"]; !ok {
		body["model"] = c.Config.Model
	}
	// 只有任务明确传 enable_thinking=false 时才发送给 AI
	// 默认不传（AI 自由决定），开启思考模式也不传
	if v, ok := body["enable_thinking"]; ok {
		if b, ok := v.(bool); ok && !b {
			// enable_thinking=false → 保留字段，发送给 AI
		} else {
			// enable_thinking=true 或未设置 → 不传
			delete(body, "enable_thinking")
		}
	}

	if _, ok := body["temperature"]; !ok {
		body["temperature"] = c.Config.Temperature
	}
	for key, value := range c.Config.Extra {
		if _, ok := body[key]; !ok {
			body[key] = value
		}
	}
	// 默认开启流式输出（可由 payload 中的 stream=false 关闭）
	if _, ok := body["stream"]; !ok {
		body["stream"] = true
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ChatResult{}, fmt.Errorf("AI 请求参数编码失败：%w", err)
	}
	// log.Printf("[AI流式调试] 请求体：%s", string(raw))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(raw))
	if err != nil {
		return ChatResult{}, fmt.Errorf("创建 AI 请求失败：%w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return ChatResult{}, fmt.Errorf("AI 服务请求失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return ChatResult{}, fmt.Errorf("AI 服务请求失败，状态码 %d，响应 %s", resp.StatusCode, preview(bodyBytes))
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		// log.Printf("[AI流式调试] 检测到 SSE 流式响应，progress=%v", c.Progress != nil)
		content, usage, err := readChatStream(resp.Body, c.Progress, c.EarlyDecision, false)
		if err != nil {
			return ChatResult{}, err
		}
		// log.Printf("[AI流式调试] 流式读取完成 content_len=%d", len(content))
		return ChatResult{Content: content, Usage: usage, ElapsedMS: int(time.Since(start).Milliseconds())}, nil
	}
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resultPayload := map[string]any{}
	if err := json.Unmarshal(bodyBytes, &resultPayload); err != nil {
		// 尝试用 SSE 方式解析（某些供应商 Content-Type 不标准）
		// log.Printf("[AI流式调试] 非 JSON 响应，尝试 SSE 方式解析，Content-Type=%s", resp.Header.Get("Content-Type"))
		if c.Progress != nil || c.EarlyDecision != nil {
			content, usage, err := readChatStream(bytes.NewReader(bodyBytes), c.Progress, c.EarlyDecision, false)
			if err == nil {
				return ChatResult{Content: content, Usage: usage, ElapsedMS: int(time.Since(start).Milliseconds())}, nil
			}
		}
		return ChatResult{}, fmt.Errorf("AI 服务返回格式不是 JSON，Content-Type=%s", resp.Header.Get("Content-Type"))
	}
	return ChatResult{
		Content:   extractChatContent(resultPayload),
		Usage:     mapValue(resultPayload["usage"]),
		ElapsedMS: int(time.Since(start).Milliseconds()),
	}, nil
}

// chat 调用 OpenAI 兼容聊天接口。
// ctx 为请求上下文，prompt 为用户提示词，temperature 为温度。
func (c *Client) chat(ctx context.Context, prompt string, temperature float64) (chatResult, error) {
	payload := map[string]any{
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"temperature":     temperature,
		"stream":          true,
		"enable_thinking": c.EnableThinking,
	}
	result, err := c.Chat(ctx, payload)
	if err != nil {
		return chatResult{}, err
	}
	return chatResult{Content: result.Content, Usage: result.Usage, ElapsedMS: result.ElapsedMS}, nil
}

// chatMessages 调用 OpenAI 兼容聊天接口，并保持稳定规则和动态内容分离。
// ctx 为请求上下文，messages 为聊天消息，temperature 为温度。
func (c *Client) chatMessages(ctx context.Context, messages []map[string]string, temperature float64) (chatResult, error) {
	result, err := c.Chat(ctx, map[string]any{
		"messages":        messages,
		"temperature":     temperature,
		"stream":          true,
		"enable_thinking": c.EnableThinking,
	})
	if err != nil {
		return chatResult{}, err
	}
	return chatResult{Content: result.Content, Usage: result.Usage, ElapsedMS: result.ElapsedMS}, nil
}

// chatResult 表示 AI 原始聊天结果。
type chatResult struct {
	Content   string
	Usage     map[string]any
	ElapsedMS int
}

// readChatStream 读取 OpenAI 兼容 SSE 流式响应。
// reader 为响应体，progress 为实时文本回调，earlyDecision 为提前评分回调。
func readChatStream(reader io.Reader, progress func(string), earlyDecision func(Decision), enableThinking bool) (string, map[string]any, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var builder strings.Builder
	var displayBuilder strings.Builder
	usage := map[string]any{}
	chunkIndex := 0
	earlySent := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		chunkIndex++
		// 日志记录流式返回内容（前 5 个分片和后续每隔 10 个分片）
		if chunkIndex <= 5 || chunkIndex%10 == 0 {
			// log.Printf("[AI流式调试] 分片#%d reasoning=%q delta=%q", chunkIndex, extractReasoningContent(payload), extractStreamDelta(payload))
		}
		if itemUsage := mapValue(payload["usage"]); len(itemUsage) > 0 {
			usage = itemUsage
		}
		delta := extractStreamDelta(payload)
		// 不管思考模式是否开启，只要有 reasoning_content 就显示
		reasoning := extractReasoningContent(payload)

		if delta == "" && reasoning == "" {
			continue
		}
		if delta != "" {
			builder.WriteString(delta)
			if earlyDecision != nil && !earlySent {
				if decision, ok := TryExtractScoreDecisionFromStream(builder.String()); ok {
					earlySent = true
					earlyDecision(decision)
				}
			}
		}
		if reasoning != "" {
			// 累积思考内容到显示用 builder（旧的+新的）
			displayBuilder.WriteString(reasoning)
		}
		if progress != nil {
			// 有思考内容显示思考，有正文显示正文
			if reasoning != "" {
				// log.Printf("[AI流式调试] 推送思考到进度回调：len=%d prev=%d new=%d", len(displayBuilder.String()), len(displayBuilder.String())-len(reasoning), len(reasoning))
				progress(displayBuilder.String())
			} else if delta != "" {
				// log.Printf("[AI流式调试] 推送正文到进度回调：len=%d", len(builder.String()))
				progress(builder.String())
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", nil, fmt.Errorf("读取 AI 流式响应失败：%w", err)
	}
	return builder.String(), usage, nil
}

// TryExtractScoreDecisionFromStream 从累计流式文本中提前提取评分结果。
// content 为当前已收到的完整文本，返回值 ok 表示已经找到包含 score 和 reason 的完整 JSON。
func TryExtractScoreDecisionFromStream(content string) (Decision, bool) {
	for _, item := range completeJSONObjects(content) {
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(item), &payload); err != nil {
			continue
		}
		if score, reason, ok := findScoreReason(payload); ok {
			return Decision{Score: clampScore(score), Reason: truncate(reason, 30)}, true
		}
	}
	return Decision{}, false
}

// completeJSONObjects 提取文本中已经闭合的 JSON 对象片段。
// content 为当前流式累计文本，会忽略字符串内部的大括号。
func completeJSONObjects(content string) []string {
	result := []string{}
	runes := []rune(content)
	for start, ch := range runes {
		if ch != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for index := start; index < len(runes); index++ {
			current := runes[index]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if current == '\\' {
					escaped = true
					continue
				}
				if current == '"' {
					inString = false
				}
				continue
			}
			if current == '"' {
				inString = true
				continue
			}
			if current == '{' {
				depth++
				continue
			}
			if current == '}' {
				depth--
				if depth == 0 {
					result = append(result, string(runes[start:index+1]))
					break
				}
			}
		}
	}
	return result
}

// findScoreReason 递归查找同一个 JSON 对象里的 score 和 reason。
// value 为 JSON 解码后的对象。
func findScoreReason(value any) (float64, string, bool) {
	switch item := value.(type) {
	case map[string]any:
		score, hasScore := numberValueOK(item["score"])
		reason := stringFromMap(item, "reason")
		if hasScore && strings.TrimSpace(reason) != "" {
			return score, reason, true
		}
		for _, child := range item {
			if score, reason, ok := findScoreReason(child); ok {
				return score, reason, true
			}
		}
	case []any:
		for _, child := range item {
			if score, reason, ok := findScoreReason(child); ok {
				return score, reason, true
			}
		}
	}
	return 0, "", false
}

// numberValueOK 将任意数字值转成 float64，并返回是否转换成功。
// value 为 JSON 字段值。
func numberValueOK(value any) (float64, bool) {
	switch item := value.(type) {
	case float64:
		return item, true
	case float32:
		return float64(item), true
	case int:
		return float64(item), true
	case int64:
		return float64(item), true
	case json.Number:
		if parsed, err := item.Float64(); err == nil {
			return parsed, true
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(item), 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// extractStreamDelta 从流式分片中提取增量文本。
// payload 为 OpenAI 兼容流式 JSON。
func extractStreamDelta(payload map[string]any) string {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	first := mapValue(choices[0])
	delta := mapValue(first["delta"])
	if content := stringFromAny(delta["content"]); content != "" {
		return content
	}
	message := mapValue(first["message"])
	if content := stringFromAny(message["content"]); content != "" {
		return content
	}
	if content := stringFromAny(first["text"]); content != "" {
		return content
	}
	return ""
}

// extractReasoningContent 从流式分片中提取思考内容（reasoning_content），用于实时显示。
func extractReasoningContent(payload map[string]any) string {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	first := mapValue(choices[0])
	delta := mapValue(first["delta"])
	if text := stringFromAny(delta["reasoning_content"]); text != "" {
		return text
	}
	return ""
}

// buildDetailMessages 构建缓存友好的详情评分消息。
// position 为岗位快照，candidate 为候选人基础信息。
func buildDetailMessages(position map[string]any, candidate map[string]any) []map[string]string {
	aiConfig := mapValue(position["ai_config"])
	system := firstNonEmpty(stringFromMap(aiConfig, "open_detail_prompt"), defaultDetailPrompt)
	return scoringMessages(system, positionDescription(position), stringFromMap(candidate, "raw_text"))
}

// buildGreetMessages 构建缓存友好的打招呼评分消息。
// position 为岗位快照，candidate 为候选人信息。
func buildGreetMessages(position map[string]any, candidate map[string]any) []map[string]string {
	aiConfig := mapValue(position["ai_config"])
	system := firstNonEmpty(stringFromMap(aiConfig, "greet_prompt"), stringFromMap(aiConfig, "filter_prompt"), stringFromMap(aiConfig, "click_prompt"), defaultGreetPrompt)
	return scoringMessages(system, positionDescription(position), stringFromMap(candidate, "raw_text"))
}

// scoringMessages 将稳定评分规则和本次变量拆成不同消息。
// system 为稳定规则，jobDesc 和 candidateText 为本次变量。
func scoringMessages(system string, jobDesc string, candidateText string) []map[string]string {
	return []map[string]string{
		{"role": "system", "content": stablePrompt(system)},
		{"role": "user", "content": "岗位要求：\n" + jobDesc + "\n\n候选人信息：\n" + candidateText},
	}
}

// stablePrompt 替换稳定提示词占位符，不混入岗位和候选人变量。
// prompt 为系统提示词。
func stablePrompt(prompt string) string {
	text := strings.TrimSpace(prompt)
	text = strings.ReplaceAll(text, "${结构化简历}", buildResumeJSONExample())
	text = strings.ReplaceAll(text, "{default_prompt}", defaultVisionSystem)
	return text
}

// buildResumeJSONExample 返回可直接入库的结构化简历 JSON 示例。
func buildResumeJSONExample() string {
	return `{
  "analysis": {"score": 80, "reason": "原因"},
  "candidate_name": "徐英",
  "birth_ym": "1990-05",
  "phone": "13800000000",
  "email": "xuying@example.com",
  "work_region": "上海",
  "work_years": "10年以上",
  "expected_salary_min": 22,
  "expected_salary_max": 30,
  "education_level": "本科",
  "expected_position": "商品经理/主管",
  "online_status": "刚刚活跃",
  "personal_description": "有快时尚品牌10年以上和户外品牌企划买手工作经验，有丰富的产品开发采购经验和供应链资源。有带领3人以上买手组团队经验，所采购产品上市30天内售罄率50%以上。",
  "work_status": "在职-月内到岗",
  "work_experiences": [
    {
      "company_name": "荟品仓",
      "position_name": "产品企划经理",
      "content": "负责商品企划、选品采购、供应商管理和产品上市节奏规划。",
      "start_ym": "2026-04",
      "end_ym": ""
    },
    {
      "company_name": "云蝠服饰",
      "position_name": "买手主管",
      "content": "负责买手团队管理、商品结构规划、供应链协同和销售表现复盘。",
      "start_ym": "2024-04",
      "end_ym": "2026-04"
    }
  ],
  "educations": [
    {
      "school_name": "陕西科技大学",
      "major_name": "服装设计与工程",
      "education_level": "本科",
      "start_ym": "2008-09",
      "end_ym": "2012-06"
    }
  ],
  "certificates": [
    {
      "certificate_name": "商品企划相关培训证书",
      "issued_by": "行业培训机构",
      "issued_ym": "2021-06"
    }
  ],
  "honors": [
    {
      "honor_name": "优秀买手主管",
      "issued_by": "云蝠服饰",
      "issued_ym": "2025-12",
      "description": "负责品类上市后售罄率表现优秀。"
    }
  ],
  "project_experiences": [
    {
      "project_name": "快时尚女装春夏商品企划",
      "role_name": "项目负责人",
      "content": "负责商品结构、价格带、供应商协同和上市节奏规划。",
      "start_ym": "2024-05",
      "end_ym": "2024-09"
    }
  ],
  "colleague_communications": [
    {
      "communicator_name": "招聘顾问",
      "communicated_at": "2026-06-30",
      "content": "候选人关注商品企划方向，接受上海机会。"
    }
  ],
}
`
}

// buildVisionSystemPrompt 构建视觉识别稳定规则。
// position 为岗位快照，可读取用户自定义视觉提示词。
func buildVisionSystemPrompt(position map[string]any) string {
	aiConfig := mapValue(position["ai_config"])
	custom := strings.TrimSpace(stringFromMap(aiConfig, "vision_prompt"))
	if custom != "" {
		return stablePrompt(custom)
	}
	return stablePrompt(defaultVisionSystem)
}

// buildVisionUserPrompt 构建视觉识别本次变量消息。
// position 为岗位快照，candidate 为候选人基础信息。
func buildVisionUserPrompt(position map[string]any, candidate map[string]any) string {
	return "岗位要求：\n" + positionDescription(position) + "\n\n候选人基础信息：\n" + stringFromMap(candidate, "raw_text")
}

// positionDescription 返回岗位要求文本。
// position 为岗位快照。
func positionDescription(position map[string]any) string {
	aiConfig := mapValue(position["ai_config"])
	if requirement := stringFromMap(aiConfig, "position_requirement"); requirement != "" {
		return requirement
	}
	parts := []string{
		stringFromMap(position, "name"),
		stringFromMap(position, "description"),
		"关键词：" + strings.Join(stringList(position["keywords"]), "、"),
		"排除词：" + strings.Join(stringList(firstPresent(position, "exclude_keywords", "exclude")), "、"),
	}
	result := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !strings.HasSuffix(part, "：") {
			result = append(result, part)
		}
	}
	return strings.Join(result, "\n")
}

// parseScoreJSON 解析 AI 输出的评分 JSON。
// content 为 AI 原始正文。
func parseScoreJSON(content string) (float64, string, error) {
	cleaned := cleanAIText(content)
	candidates := []string{cleaned}
	re := regexp.MustCompile(`(?s)\{.*\}`)
	if match := re.FindString(cleaned); match != "" {
		candidates = append(candidates, match)
	}
	for _, item := range candidates {
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(item), &payload); err != nil {
			continue
		}
		return numberValue(payload["score"], 0), stringFromMap(payload, "reason"), nil
	}
	return 0, "", fmt.Errorf("AI 返回不是合法 JSON")
}

// parseVisionScoreJSON 解析图片详情 AI 输出的评分和详情文本（无 resume 数据版本，向后兼容）。
// content 为 AI 原始正文。
func parseVisionScoreJSON(content string) (float64, string, string, error) {
	score, reason, detailText, _, err := parseVisionScoreJSONWithResume(content)
	return score, reason, detailText, err
}

// parseVisionScoreJSONWithResume 解析图片详情 AI 输出的评分、详情文本和结构化简历。
// 只支持新版扁平简历模型，ai.greet 中保存第二次分析分数和原因。
// content 为 AI 原始正文。
// 返回 score, reason, detailText, resumeData, error。
// resumeData 可能为 nil（如果 AI 没有返回 resume 字段）。
func parseVisionScoreJSONWithResume(content string) (float64, string, string, map[string]any, error) {
	cleaned := cleanAIText(content)
	candidates := []string{cleaned}
	re := regexp.MustCompile(`(?s)\{.*\}`)
	if match := re.FindString(cleaned); match != "" {
		candidates = append(candidates, match)
	}
	for _, item := range candidates {
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(item), &payload); err != nil {
			continue
		}
		ai := mapValue(payload["ai"])
		greet := mapValue(ai["greet"])
		score := numberValue(greet["score"], 0)
		reason := stringFromMap(greet, "reason")
		return score, reason, stringFromMap(payload, "raw_text"), normalizeResumePayload(payload), nil
	}
	return 0, "", "", nil, fmt.Errorf("AI 返回不是合法 JSON")
}

// normalizeResumePayload 只保留可直接入库的标准简历字段。
// payload 为 AI 返回 JSON。
func normalizeResumePayload(payload map[string]any) map[string]any {
	result := map[string]any{}
	for _, key := range []string{"candidate_name", "birth_ym", "phone", "email", "work_region", "work_years", "expected_salary_min", "expected_salary_max", "education_level", "expected_position", "online_status", "personal_description", "work_status", "work_experiences", "educations", "certificates", "honors", "project_experiences", "colleague_communications", "raw_text"} {
		if value, ok := payload[key]; ok {
			result[key] = value
		}
	}
	ai := mapValue(payload["ai"])
	if detail := mapValue(ai["detail"]); len(detail) > 0 {
		result["ai_detail_score"] = detail["score"]
		result["ai_detail_reason"] = stringFromMap(detail, "reason")
	}
	if greet := mapValue(ai["greet"]); len(greet) > 0 {
		result["ai_greet_score"] = greet["score"]
		result["ai_greet_reason"] = stringFromMap(greet, "reason")
	}
	return result
}

// extractChatContent 提取 OpenAI 兼容响应正文。
// payload 为 AI 响应 JSON。
func extractChatContent(payload map[string]any) string {
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return stringFromMap(payload, "content")
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	message := mapValue(first["message"])
	return firstNonEmpty(stringFromMap(message, "content"), stringFromMap(first, "text"))
}

// chatCompletionsURL 生成 OpenAI 兼容 chat/completions 地址。
// baseURL 为用户填写的接口地址。
func chatCompletionsURL(baseURL string) string {
	value := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, "/chat/completions") {
		return value
	}
	if strings.HasSuffix(value, "/v1") {
		return value + "/chat/completions"
	}
	return value + "/v1/chat/completions"
}

// cleanAIText 清理 AI 输出中的 Markdown 包裹。
// content 为 AI 输出。
func cleanAIText(content string) string {
	text := strings.TrimSpace(content)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

// numberFromAIConfig 从岗位 AI 配置中读取数字。
// position 为岗位快照，fallback 为默认值，keys 为字段名。
func numberFromAIConfig(position map[string]any, fallback float64, keys ...string) float64 {
	aiConfig := mapValue(position["ai_config"])
	for _, key := range keys {
		if value, ok := aiConfig[key]; ok {
			return numberValue(value, fallback)
		}
	}
	return fallback
}

// numberValue 将任意值转换为浮点数。
// value 为原始值，fallback 为默认值。
func numberValue(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(typed, "%f", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

// clampScore 将评分限制在 0 到 100。
// score 为原始评分。
func clampScore(score float64) float64 {
	return math.Max(0, math.Min(100, score))
}

// truncate 截断文本。
// text 为原始文本，limit 为最大长度。
func truncate(text string, limit int) string {
	value := strings.TrimSpace(text)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

// mapValue 将任意值转换为 map。
// value 为原始值。
func mapValue(value any) map[string]any {
	if item, ok := value.(map[string]any); ok && item != nil {
		return item
	}
	return map[string]any{}
}

// stringFromMap 从 map 中读取字符串。
// item 为原始字典，key 为字段名。
func stringFromMap(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	return stringFromAny(item[key])
}

// stringFromAny 将任意值转换为字符串。
// value 为原始值。
func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

// stringList 将任意值转换为字符串列表。
// value 为原始值。
func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := []string{}
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	case string:
		result := []string{}
		for _, item := range strings.Fields(strings.ReplaceAll(typed, ",", " ")) {
			if strings.TrimSpace(item) != "" {
				result = append(result, strings.TrimSpace(item))
			}
		}
		return result
	default:
		return []string{}
	}
}

// firstPresent 返回第一个存在的 map 字段值。
// item 为原始字典，keys 为候选字段名。
func firstPresent(item map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			return value
		}
	}
	return nil
}

// firstNonEmpty 返回第一个非空字符串。
// values 为候选字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// preview 返回错误响应预览文本。
// body 为响应体。
func preview(body []byte) string {
	text := strings.ReplaceAll(string(body), "\n", " ")
	if len([]rune(text)) > 500 {
		return string([]rune(text)[:500])
	}
	return text
}
