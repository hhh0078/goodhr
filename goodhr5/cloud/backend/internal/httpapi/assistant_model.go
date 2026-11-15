// 本文件调用用户内置聊天模型，并逐条验证模型 JSON 事件；原始思维链和不合法内容不向客户端透传。
package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// assistantModelEvent 是模型唯一可接受的输出合同，工具参数使用后端独立白名单校验。
type assistantModelEvent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// assistantFatalError 表示重试无法安全修复的账号、余额、权限或不确定付费结果。
type assistantFatalError struct{ message string }

// Error 返回可展示中文错误，不带 URL、Key 或上游原始正文。
func (e assistantFatalError) Error() string { return e.message }

// strictAssistantJSON 拒绝外围文字、重复字段、未知字段、非法 UTF8 和拼接对象。
func strictAssistantJSON(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return errors.New("JSON 包含无效文字编码")
	}
	check := json.NewDecoder(bytes.NewReader(raw))
	check.UseNumber()
	if err := assistantUniqueJSON(check); err != nil {
		return err
	}
	if _, err := check.Token(); err != io.EOF {
		return errors.New("只允许一个完整 JSON 对象")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("JSON 字段不符合合同：%w", err)
	}
	return nil
}

// assistantUniqueJSON 递归检查 JSON 容器中的重复键，防止模型和执行器对同一参数产生不同理解。
func assistantUniqueJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter == '{' {
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("JSON 存在重复字段")
			}
			seen[name] = true
			if err = assistantUniqueJSON(decoder); err != nil {
				return err
			}
		}
	} else if delimiter == '[' {
		for decoder.More() {
			if err = assistantUniqueJSON(decoder); err != nil {
				return err
			}
		}
	} else {
		return errors.New("JSON 容器不完整")
	}
	_, err = decoder.Token()
	return err
}

// builtinForScope 校验当前用户确实使用本站内置 Key，防止只伪造 URL 绕过钱包。
func (s *AssistantService) builtinForScope(scope CandidateLibraryScope) (AIConfig, error) {
	if !aiSearchEnabled() {
		return AIConfig{}, assistantFatalError{"AI招聘助手暂未启用"}
	}
	cfg, err := s.wallet.aiConfigs.UserConfig(scope.UserEmail)
	if err != nil || !cfg.Enabled || !isCurrentSiteAIURL(cfg.BaseURL) {
		return AIConfig{}, assistantFatalError{"请先在个人配置中选择使用内置 AI，再来找我干活"}
	}
	email, err := s.wallet.wallet.UserEmailByAIKey(cfg.APIKey)
	if err != nil || !strings.EqualFold(email, scope.UserEmail) {
		return AIConfig{}, assistantFatalError{"内置 AI 配置还没接好，请重新选择使用内置 AI"}
	}
	balance, err := s.wallet.wallet.BalanceUnits(scope.UserEmail)
	if err != nil || balance <= 0 {
		return AIConfig{}, assistantFatalError{"AI 余额不足，请充值后再试"}
	}
	return cfg, nil
}

// callModel 使用原内置配置和原钱包扣费；每条完整 JSON 通过验证后才交给调用方。
func (s *AssistantService) callModel(ctx context.Context, scope CandidateLibraryScope, messages []AIMsg, emit func(assistantModelEvent) error) error {
	cfg, err := s.builtinForScope(scope)
	if err != nil {
		return err
	}
	platform, err := s.wallet.loadBuiltinAIConfig()
	if err != nil {
		return assistantFatalError{"内置 AI 暂时没准备好，请稍后再试"}
	}
	model, ok := platform.modelByID(cfg.Model)
	if !ok {
		return assistantFatalError{"聊天模型配置已变化，请重新保存个人 AI 配置"}
	}
	body, _ := json.Marshal(map[string]any{"model": model.ID, "messages": messages, "temperature": 0.2, "stream": true, "stream_options": map[string]bool{"include_usage": true}, "enable_thinking": false, "max_tokens": 8192})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeAIChatCompletionsURL(platform.UpstreamBaseURL), bytes.NewReader(body))
	if err != nil {
		return assistantFatalError{"内置 AI 地址未配置完整"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+platform.UpstreamAPIKey)
	response, err := s.wallet.httpClient.Do(request)
	if err != nil {
		return assistantFatalError{"AI 连接中断，付费结果未确认；请核对后重试"}
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 402 {
		return assistantFatalError{"AI 认证或余额不足，请检查配置后再试"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("AI 服务返回状态 %d", response.StatusCode)
	}
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 8<<20))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var pending strings.Builder
	prompt, completion := 0, 0
	finished := false
	var protocolError error
	var readError error
	count := 0
	// accept 只接受一条完整事件，坏事件不产生任何可执行效果。
	accept := func(line string) {
		if protocolError != nil {
			return
		}
		if strings.TrimSpace(line) == "" {
			return
		}
		var event assistantModelEvent
		if err := strictAssistantJSON([]byte(line), &event); err != nil {
			protocolError = errors.New("模型没有严格返回 JSON 事件，请按合同纠正")
			return
		}
		if err := validateAssistantModelEvent(event); err != nil {
			protocolError = err
			return
		}
		count++
		if count > 200 {
			protocolError = errors.New("模型本轮事件过多，请缩短回答")
			return
		}
		if err := emit(event); err != nil {
			protocolError = err
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			finished = true
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			readError = errors.New("AI 流式响应损坏")
			continue
		}
		if chunk.Usage != nil {
			prompt = chunk.Usage.Prompt
			completion = chunk.Usage.Completion
		}
		for _, choice := range chunk.Choices {
			pending.WriteString(choice.Delta.Content)
			if pending.Len() > 1<<20 {
				protocolError = errors.New("模型单条 JSON 过长")
			}
			for strings.Contains(pending.String(), "\n") {
				current := pending.String()
				split := strings.IndexByte(current, '\n')
				accept(current[:split])
				pending.Reset()
				pending.WriteString(current[split+1:])
			}
			if choice.FinishReason != nil && *choice.FinishReason == "length" {
				protocolError = errors.New("模型 JSON 被长度限制截断，请缩短回答")
			}
		}
	}
	if err = scanner.Err(); err != nil {
		readError = assistantFatalError{"AI 流中断，付费结果未确认；请核对后重试"}
	}
	if prompt < 0 || completion < 0 || prompt+completion <= 0 {
		return assistantFatalError{"AI 没有返回可靠用量，结果暂不采用，请核对服务配置"}
	}
	if err = s.wallet.chargeAIUsage(scope.UserEmail, model, prompt, completion); err != nil {
		return assistantFatalError{"AI 已返回，但计费回执未保存，请核对后重试"}
	}
	if readError != nil {
		return readError
	}
	if !finished {
		return assistantFatalError{"AI 流未正常结束，请核对费用后重试"}
	}
	accept(pending.String())
	if protocolError != nil {
		return protocolError
	}
	if count == 0 {
		return errors.New("模型没有返回 JSON 事件")
	}
	return nil
}

// validateAssistantModelEvent 校验事件类型及字段关系，模型不能注入任意工具或隐藏指令。
func validateAssistantModelEvent(event assistantModelEvent) error {
	switch event.Type {
	case "summary", "text":
		if strings.TrimSpace(event.Text) == "" || len([]rune(event.Text)) > 3000 || event.Name != "" || len(event.Arguments) > 0 {
			return errors.New("文字事件字段不符合 JSON 合同")
		}
	case "tool":
		if event.Text != "" || (event.Name != "search_positions" && event.Name != "search_candidates") || len(event.Arguments) == 0 || len(event.Arguments) > 20000 {
			return errors.New("工具不在允许范围或参数不完整")
		}
		if event.Arguments[0] != '{' {
			return errors.New("工具参数必须是 JSON 对象")
		}
	case "done":
		if event.Text != "" || event.Name != "" || len(event.Arguments) > 0 {
			return errors.New("结束事件只能包含 type 字段")
		}
	default:
		return errors.New("模型事件类型不在允许范围")
	}
	return nil
}
