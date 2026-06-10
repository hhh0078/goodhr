// Package localai 负责使用本地保存的 AI 配置调用 OpenAI 兼容接口。
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
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/localdb"
)

const (
	defaultGreetThreshold  = 70.0
	defaultDetailThreshold = 60.0
	defaultGreetPrompt     = `你是一个资深的HR专家。
请根据岗位要求给候选人打“打招呼建议分”。

重要提示：
1. 仅输出 JSON，不能输出其它内容。
2. 返回字段必须是 score 和 reason。
3. score 范围是 0-100，可以是小数。
4. reason 控制在30字以内。
5. 禁止输出 Markdown，禁止输出 Markdown 代码块。

岗位要求：
{job_desc}

候选人信息：
{candidate_text}

请返回JSON：{"score": 78, "reason": "匹配核心要求"}`
	defaultDetailPrompt = `你是一个资深的HR专家。
请根据岗位要求给候选人打“查看详情建议分”。

重要提示：
1. 仅根据候选人基础信息评估是否值得打开详情。
2. 仅输出 JSON，不能输出其它内容。
3. 返回字段必须是 score 和 reason。
4. score 范围是 0-100，可以是小数。
5. reason 控制在30字以内。
6. 禁止输出 Markdown，禁止输出 Markdown 代码块。

岗位要求：
{job_desc}

候选人基础信息：
{candidate_text}

请返回JSON：{"score": 66, "reason": "可进一步确认细节"}`
)

// Client 是本地 AI 调用客户端。
type Client struct {
	Config          localdb.AIConfig
	HTTPClient      *http.Client
	Progress        func(string)
	EnableThinking  bool // 开启后显示 reasoning_content 流式思考
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

// ScoreForDetail 给候选人计算查看详情评分。
// ctx 为请求上下文，position 为岗位快照，candidate 为候选人基础信息。
func (c *Client) ScoreForDetail(ctx context.Context, position map[string]any, candidate map[string]any) (Decision, error) {
	threshold := numberFromAIConfig(position, defaultDetailThreshold, "detail_score_threshold", "open_detail_threshold", "detail_threshold")
	prompt := buildDetailPrompt(position, candidate)
	result, err := c.chat(ctx, prompt, numberFromAIConfig(position, c.Config.Temperature, "temperature"))
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
	prompt := buildGreetPrompt(position, candidate)
	result, err := c.chat(ctx, prompt, numberFromAIConfig(position, c.Config.Temperature, "temperature"))
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
	prompt := buildVisionGreetPrompt(position, candidate)
	content := []map[string]any{
		{"type": "text", "text": prompt},
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)}},
	}
	result, err := c.Chat(ctx, map[string]any{
		"messages":    []map[string]any{{"role": "user", "content": content}},
		"temperature": 0.1,
	})
	if err != nil {
		return Decision{}, err
	}
	score, reason, detailText, err := parseVisionScoreJSON(result.Content)
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
	if _, ok := body["temperature"]; !ok {
		body["temperature"] = c.Config.Temperature
	}
	for key, value := range c.Config.Extra {
		if _, ok := body[key]; !ok {
			body[key] = value
		}
	}
	if c.Progress != nil {
		body["stream"] = true
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ChatResult{}, fmt.Errorf("AI 请求参数编码失败：%w", err)
	}
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
	if c.Progress != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		content, usage, err := readChatStream(resp.Body, c.Progress, c.EnableThinking)
		if err != nil {
			return ChatResult{}, err
		}
		return ChatResult{Content: content, Usage: usage, ElapsedMS: int(time.Since(start).Milliseconds())}, nil
	}
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resultPayload := map[string]any{}
	if err := json.Unmarshal(bodyBytes, &resultPayload); err != nil {
		return ChatResult{}, fmt.Errorf("AI 服务返回格式不是 JSON")
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
	result, err := c.Chat(ctx, map[string]any{
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": temperature,
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
// reader 为响应体，progress 为实时文本回调。
func readChatStream(reader io.Reader, progress func(string), enableThinking bool) (string, map[string]any, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var builder strings.Builder
	var displayBuilder strings.Builder
	usage := map[string]any{}
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
		if itemUsage := mapValue(payload["usage"]); len(itemUsage) > 0 {
			usage = itemUsage
		}
		delta := extractStreamDelta(payload)
		var reasoning string
		if enableThinking {
			// 开启思考模式时才提取 reasoning_content
			reasoning = extractReasoningContent(payload)
		}

		if delta == "" && reasoning == "" {
			continue
		}
		if delta != "" {
			builder.WriteString(delta)
		}
		if reasoning != "" {
			// 累积思考内容到显示用 builder（旧的+新的）
			displayBuilder.WriteString(reasoning)
		}
		if progress != nil {
			// 显示累积的思考内容或最终回复
			if reasoning != "" {
				progress(displayBuilder.String())
			} else if delta != "" {
				progress(displayBuilder.String())
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", nil, fmt.Errorf("读取 AI 流式响应失败：%w", err)
	}
	return builder.String(), usage, nil
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

// buildDetailPrompt 构建查看详情评分提示词。
// position 为岗位快照，candidate 为候选人基础信息。
func buildDetailPrompt(position map[string]any, candidate map[string]any) string {
	aiConfig := mapValue(position["ai_config"])
	custom := stringFromMap(aiConfig, "open_detail_prompt")
	jobDesc := positionDescription(position)
	candidateText := firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text"))
	fallback := strings.ReplaceAll(defaultDetailPrompt, "{job_desc}", jobDesc)
	fallback = strings.ReplaceAll(fallback, "{candidate_text}", candidateText)
	if custom == "" {
		return fallback
	}
	return templatePrompt(custom, jobDesc, candidateText, fallback)
}

// buildGreetPrompt 构建打招呼评分提示词。
// position 为岗位快照，candidate 为候选人信息。
func buildGreetPrompt(position map[string]any, candidate map[string]any) string {
	aiConfig := mapValue(position["ai_config"])
	custom := firstNonEmpty(
		stringFromMap(aiConfig, "greet_prompt"),
		stringFromMap(aiConfig, "filter_prompt"),
		stringFromMap(aiConfig, "click_prompt"),
	)
	jobDesc := positionDescription(position)
	candidateText := firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text"))
	fallback := strings.ReplaceAll(defaultGreetPrompt, "{job_desc}", jobDesc)
	fallback = strings.ReplaceAll(fallback, "{candidate_text}", candidateText)
	if custom == "" {
		return fallback
	}
	return templatePrompt(custom, jobDesc, candidateText, fallback)
}

// buildVisionGreetPrompt 构建图片详情识别和打招呼评分提示词。
// position 为岗位快照，candidate 为候选人基础信息。
func buildVisionGreetPrompt(position map[string]any, candidate map[string]any) string {
	jobDesc := positionDescription(position)
	candidateText := firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text"))
	return `你是一个资深的HR专家。
请根据岗位要求、候选人基础信息，以及图片中的候选人详情，直接完成打招呼评分。

重要提示：
1. 你必须先识别图片中的候选人详情，再结合岗位要求评分。
2. 仅输出 JSON，不能输出其它内容。
3. 返回字段必须包含 score、reason、detail_text。
4. score 范围是 0-100，可以是小数。
5. reason 控制在30字以内。
6. detail_text 输出从图片中识别到的中文详情文本。
7. 禁止输出 Markdown，禁止输出 Markdown 代码块。

岗位要求：
` + jobDesc + `

候选人基础信息：
` + candidateText + `

请返回JSON：{"score": 78, "reason": "匹配核心要求", "detail_text": "图片识别到的候选人详情"}`
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

// templatePrompt 替换自定义提示词变量。
// template 为自定义模板，jobDesc 为岗位要求，candidateText 为候选人信息。
func templatePrompt(template string, jobDesc string, candidateText string, fallback string) string {
	replacements := map[string]string{
		"${岗位信息}":          jobDesc,
		"${候选人信息}":         candidateText,
		"{{岗位信息}}":         jobDesc,
		"{{候选人信息}}":        candidateText,
		"{job_desc}":       jobDesc,
		"{candidate_text}": candidateText,
		"{default_prompt}": fallback,
	}
	prompt := strings.TrimSpace(template)
	for key, value := range replacements {
		prompt = strings.ReplaceAll(prompt, key, value)
	}
	if !strings.Contains(prompt, jobDesc) || !strings.Contains(prompt, candidateText) {
		prompt += "\n\n岗位要求：\n" + jobDesc + "\n\n候选人信息：\n" + candidateText
	}
	return prompt
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

// parseVisionScoreJSON 解析图片详情 AI 输出的评分和详情文本。
// content 为 AI 原始正文。
func parseVisionScoreJSON(content string) (float64, string, string, error) {
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
		detailText := firstNonEmpty(
			stringFromMap(payload, "detail_text"),
			stringFromMap(payload, "candidate_detail"),
			stringFromMap(payload, "text"),
		)
		return numberValue(payload["score"], 0), stringFromMap(payload, "reason"), detailText, nil
	}
	return 0, "", "", fmt.Errorf("AI 返回不是合法 JSON")
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
