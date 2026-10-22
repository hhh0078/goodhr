// 本文件提供内置向量接口和后台向量调用，复用用户钱包，严格核对向量及用量后才扣费，不保存简历正文或密钥。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	builtinEmbeddingModel                = "qwen3.7-text-embedding-flash"
	embeddingInputUnitsPerMillion  int64 = 1250
	embeddingOutputUnitsPerMillion int64 = 0
	maxEmbeddingBatch                    = 20
)

// embeddingRequest 只接受固定模型和纯文字输入，禁止调用者覆盖上游地址、密钥或价格。
type embeddingRequest struct {
	Model          string          `json:"model,omitempty"`
	Input          json.RawMessage `json:"input"`
	EncodingFormat string          `json:"encoding_format,omitempty"`
}

// embeddingResponse 保存已核对的 OpenAI 兼容向量响应；输出用量缺失时明确按零计费。
type embeddingResponse struct {
	Object string          `json:"object"`
	Model  string          `json:"model"`
	Data   []embeddingData `json:"data"`
	Usage  embeddingUsage  `json:"usage"`
}

// embeddingData 使用明确索引对应输入，避免服务商返回乱序时把向量绑定到别人的经历。
type embeddingData struct {
	Object    string          `json:"object"`
	Index     *int            `json:"index"`
	Embedding embeddingVector `json:"embedding"`
}

// embeddingVector 拒绝向量中的null、文字和混合类型，避免JSON解码把缺失数字悄悄变为零。
type embeddingVector []float64

// UnmarshalJSON 将完整数字数组转换为向量，raw 为上游数组，返回错误时不允许收费。
func (v *embeddingVector) UnmarshalJSON(raw []byte) error {
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil || len(values) == 0 {
		return errors.New("向量必须是非空数字数组")
	}
	result := make([]float64, len(values))
	for i, value := range values {
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return errors.New("向量包含非数字内容")
		}
		result[i] = number
	}
	*v = result
	return nil
}

// embeddingUsage 使用指针区分缺失值和零，禁止缺失用量被误认为免费成功。
type embeddingUsage struct {
	PromptTokens     *int `json:"prompt_tokens"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
	TotalTokens      *int `json:"total_tokens"`
}

// Embeddings 处理内置向量请求；r 必须携带本人内置 AI Key，校验失败不会调用模型或扣款。
func (s *AIWalletService) Embeddings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "请使用 POST 提交向量文字")
		return
	}
	if s == nil || s.wallet == nil {
		writeError(w, http.StatusServiceUnavailable, "向量服务暂时没准备好")
		return
	}
	key := bearerToken(r.Header.Get("Authorization"))
	email, err := s.wallet.UserEmailByAIKey(key)
	if key == "" || err != nil || email == "" {
		writeError(w, http.StatusUnauthorized, "内置 AI Key 没核对上，请检查个人配置")
		return
	}
	var input embeddingRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "请提交一个完整的向量 JSON 请求")
		return
	}
	if (input.Model != "" && input.Model != builtinEmbeddingModel) || (input.EncodingFormat != "" && input.EncodingFormat != "float") {
		writeError(w, http.StatusBadRequest, "向量接口只使用已配置的向量模型和数字格式")
		return
	}
	texts, err := parseEmbeddingInput(input.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.embedForUser(r.Context(), email, texts)
	if err != nil {
		writeError(w, embeddingErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// EmbedForUser 为后台检索生成按输入顺序排列的向量，email 必须来自已认证身份，不可使用模型或浏览器提供的邮箱。
func (s *AIWalletService) EmbedForUser(ctx context.Context, email string, texts []string) ([][]float64, error) {
	result, err := s.embedForUser(ctx, email, texts)
	if err != nil {
		return nil, err
	}
	vectors := make([][]float64, len(result.Data))
	for _, item := range result.Data {
		vectors[*item.Index] = item.Embedding
	}
	return vectors, nil
}

// embeddingCallError 区分配置、余额和上游错误，错误文字只使用固定提示，不回显上游正文。
type embeddingCallError struct {
	status  int
	message string
}

// Error 返回不含密钥和简历正文的可展示错误。
func (e *embeddingCallError) Error() string { return e.message }

// embeddingErrorStatus 返回接口可以采用的明确状态码，未知错误统一视为上游故障。
func embeddingErrorStatus(err error) int {
	var target *embeddingCallError
	if errors.As(err, &target) {
		return target.status
	}
	return http.StatusBadGateway
}

// parseEmbeddingInput 校验单条或批量文字，限制总量，拒绝空白、数字Token数组和混合类型。
func parseEmbeddingInput(raw json.RawMessage) ([]string, error) {
	var text string
	var texts []string
	if json.Unmarshal(raw, &text) == nil {
		texts = []string{text}
	} else if json.Unmarshal(raw, &texts) != nil {
		return nil, errors.New("向量输入请使用文字或文字数组")
	}
	if err := validateEmbeddingTexts(texts); err != nil {
		return nil, err
	}
	return texts, nil
}

// validateEmbeddingTexts 同时限制每段与整批文字，返回错误时不得继续调用付费模型。
func validateEmbeddingTexts(texts []string) error {
	if len(texts) == 0 || len(texts) > maxEmbeddingBatch {
		return errors.New("每批向量请提交1到20段文字")
	}
	total := 0
	for _, text := range texts {
		n := utf8.RuneCountInString(text)
		if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || n > 16000 {
			return errors.New("每段向量文字请保持非空且不超过16000字")
		}
		total += n
	}
	if total > 128000 {
		return errors.New("本批向量文字超过128000字，请分批提交")
	}
	return nil
}

// embedForUser 读取本人站内配置，校验响应后复用分笔钱包扣款；网络不明时由上层显式决定是否重试。
func (s *AIWalletService) embedForUser(ctx context.Context, email string, texts []string) (embeddingResponse, error) {
	var empty embeddingResponse
	if err := validateEmbeddingTexts(texts); err != nil {
		return empty, &embeddingCallError{http.StatusBadRequest, err.Error()}
	}
	if s == nil || s.aiConfigs == nil || s.wallet == nil {
		return empty, &embeddingCallError{http.StatusServiceUnavailable, "向量服务暂时没准备好"}
	}
	user, err := s.aiConfigs.UserConfig(email)
	if err != nil || !user.Enabled || !isCurrentSiteAIURL(user.BaseURL) || strings.TrimSpace(user.APIKey) == "" {
		return empty, &embeddingCallError{http.StatusConflict, "请先启用当前 GoodHR 站点的内置 AI 配置，再生成向量"}
	}
	owner, err := s.wallet.UserEmailByAIKey(user.APIKey)
	if err != nil || !strings.EqualFold(owner, email) {
		return empty, &embeddingCallError{http.StatusForbidden, "内置 AI 配置和当前账号没有核对上"}
	}
	balance, err := s.wallet.BalanceUnits(email)
	if err != nil {
		return empty, &embeddingCallError{http.StatusServiceUnavailable, "AI 余额暂时没查清楚，请稍后再试"}
	}
	if balance <= 0 {
		return empty, &embeddingCallError{http.StatusPaymentRequired, "AI 余额有点紧张，请充值后再生成向量"}
	}
	config, err := s.loadBuiltinAIConfig()
	if err != nil || strings.TrimSpace(config.UpstreamAPIKey) == "" {
		return empty, &embeddingCallError{http.StatusServiceUnavailable, "内置向量服务还没配置完整"}
	}
	endpoint, err := normalizeAIEmbeddingsURL(config.UpstreamBaseURL)
	if err != nil {
		return empty, &embeddingCallError{http.StatusServiceUnavailable, "内置向量地址没有配置完整"}
	}
	payload, _ := json.Marshal(map[string]any{"model": builtinEmbeddingModel, "input": texts, "encoding_format": "float"})
	callCtx, cancel := context.WithTimeout(ctx, aiRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return empty, &embeddingCallError{http.StatusServiceUnavailable, "内置向量地址暂时不可用"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(config.UpstreamAPIKey))
	response, err := s.httpClient.Do(request)
	if err != nil {
		return empty, errors.New("向量响应未确认，可能产生上游费用；请核对后再试")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return empty, errors.New("向量服务没有完成请求，请检查模型渠道和余额后重试")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAICompatibleBodyBytes+1))
	if err != nil || len(body) > maxAICompatibleBodyBytes {
		return empty, errors.New("向量响应没有读完整，本次没有从钱包扣费")
	}
	var result embeddingResponse
	if json.Unmarshal(body, &result) != nil {
		return empty, errors.New("向量服务没有返回完整 JSON，本次没有从钱包扣费")
	}
	prompt, completion, err := validateEmbeddingResponse(result, len(texts))
	if err != nil {
		return empty, err
	}
	cost, err := embeddingCostUnits(prompt, completion)
	if err != nil {
		return empty, err
	}
	if _, err = s.wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: -cost, Category: "ai_usage", Reason: "内置向量调用扣费", ModelID: builtinEmbeddingModel, PromptTokens: prompt, CompletionTokens: completion}); err != nil {
		return empty, errors.New("向量已返回，但扣费回执没有确认，请先核对账本后再试")
	}
	return result, nil
}

// normalizeAIEmbeddingsURL 将既有聊天上游地址切换为同一服务的向量地址，保留兼容路径前缀，不接受用户身份片段。
func normalizeAIEmbeddingsURL(raw string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return "", errors.New("向量地址无效")
	}
	path := strings.TrimRight(endpoint.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		path = strings.TrimSuffix(path, "/chat/completions") + "/embeddings"
	case strings.HasSuffix(path, "/embeddings"):
	case strings.HasSuffix(path, "/v1"):
		path += "/embeddings"
	default:
		path += "/v1/embeddings"
	}
	endpoint.Path, endpoint.RawPath, endpoint.Fragment = path, "", ""
	return endpoint.String(), nil
}

// validateEmbeddingResponse 校验模型、索引、维度、数值和可信用量；失败时禁止保存向量与扣款。
func validateEmbeddingResponse(result embeddingResponse, count int) (int, int, error) {
	invalid := errors.New("向量结果或用量没有核对通过，本次没有从钱包扣费")
	if result.Model != builtinEmbeddingModel || len(result.Data) != count || result.Usage.PromptTokens == nil || result.Usage.TotalTokens == nil {
		return 0, 0, invalid
	}
	prompt, completion := *result.Usage.PromptTokens, 0
	if result.Usage.CompletionTokens != nil {
		completion = *result.Usage.CompletionTokens
	}
	if prompt <= 0 || prompt > 10_000_000 || completion < 0 || completion > 10_000_000 || *result.Usage.TotalTokens != prompt+completion {
		return 0, 0, invalid
	}
	seen, dimensions := map[int]bool{}, 0
	for _, item := range result.Data {
		if item.Index == nil || *item.Index < 0 || *item.Index >= count || seen[*item.Index] || len(item.Embedding) != 1024 {
			return 0, 0, invalid
		}
		seen[*item.Index] = true
		if dimensions == 0 {
			dimensions = len(item.Embedding)
		}
		if len(item.Embedding) != dimensions {
			return 0, 0, invalid
		}
		nonzero := false
		for _, value := range item.Embedding {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return 0, 0, invalid
			}
			if value != 0 {
				nonzero = true
			}
		}
		if !nonzero {
			return 0, 0, invalid
		}
	}
	return prompt, completion, nil
}

// embeddingCostUnits 使用整数价格计算费用，按既有钱包的0.0001元单位向上取整，避免小数分价格被截断。
func embeddingCostUnits(prompt, completion int) (int64, error) {
	if prompt < 0 || completion < 0 || prompt > 10_000_000 || completion > 10_000_000 {
		return 0, fmt.Errorf("向量用量超出可核对范围")
	}
	numerator := int64(prompt)*embeddingInputUnitsPerMillion + int64(completion)*embeddingOutputUnitsPerMillion
	return (numerator + 999999) / 1_000_000, nil
}
