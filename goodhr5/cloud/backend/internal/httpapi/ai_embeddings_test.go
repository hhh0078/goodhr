// 本文件使用合成上游验证向量输入、模型与维度合同、用量收费及站内配置权限，不连接真实模型或生产环境。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// embeddingFixture 创建本人内置配置与余额，transport 用于替代付费上游，返回服务及测试钱包。
func embeddingFixture(t *testing.T, transport http.RoundTripper) (*AIWalletService, *MemoryAIWalletStore) {
	t.Helper()
	t.Setenv("GOODHR_AI_SEARCH_ENABLED", "1")
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	system := NewMemorySystemConfigStore()
	saveBuiltinAIConfigForTest(t, system, builtinAIPolicyConfigForTest())
	configs := NewMemoryAIConfigStore()
	_, err := configs.SaveUserConfig("embedding@example.com", AIConfig{BaseURL: "https://goodhr.example.com/api/ai-compatible/v1/chat/completions", APIKey: "embedding-user-key", Model: "first-model", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	wallet := NewMemoryAIWalletStore()
	wallet.BindAIKey("embedding-user-key", "embedding@example.com")
	if _, err = wallet.AdjustBalance(AIWalletRecord{UserEmail: "embedding@example.com", ChangeUnits: 100000, Category: "signup_bonus"}); err != nil {
		t.Fatal(err)
	}
	service := NewAIWalletService(nil, wallet, configs, system)
	service.httpClient = &http.Client{Transport: transport}
	return service, wallet
}

// embeddingResultForTest 生成1024维、默认仅输入用量的合法合成响应，indices可用于测试上游乱序。
func embeddingResultForTest(indices ...int) embeddingResponse {
	prompt, total := 1000000, 1000000
	result := embeddingResponse{Object: "list", Model: builtinEmbeddingModel, Usage: embeddingUsage{PromptTokens: &prompt, TotalTokens: &total}}
	for _, index := range indices {
		vector := make([]float64, 1024)
		vector[0] = float64(index + 1)
		value := index
		result.Data = append(result.Data, embeddingData{Object: "embedding", Index: &value, Embedding: vector})
	}
	return result
}

// embeddingHTTPResponse 返回与真实服务结构相同的JSON响应，不输出测试密钥。
func embeddingHTTPResponse(result embeddingResponse) *http.Response {
	body, _ := json.Marshal(result)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}
}

// TestEmbeddingsUsesUserWallet 验证固定向量模型、同渠道Key、准确价格、乱序索引还原及聊天默认模型保持原值。
func TestEmbeddingsUsesUserWallet(t *testing.T) {
	service, wallet := embeddingFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://upstream.example.com/v1/embeddings" || r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Fatal("上游地址或身份未复用")
		}
		var input struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.Model != builtinEmbeddingModel || len(input.Input) != 2 {
			t.Fatal("没有按向量协议调用")
		}
		return embeddingHTTPResponse(embeddingResultForTest(1, 0)), nil
	}))
	vectors, err := service.EmbedForUser(context.Background(), "embedding@example.com", []string{"第一段经历", "第二段经历"})
	if err != nil || len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 2 {
		t.Fatalf("乱序结果没有归位: err=%v", err)
	}
	balance, _ := wallet.BalanceUnits("embedding@example.com")
	if balance != 100000-1250 {
		t.Fatalf("费用=%d", 100000-balance)
	}
	records, _, _ := wallet.ListRecords("embedding@example.com", 10, 0)
	if records[0].Category != "ai_usage" || records[0].ModelID != builtinEmbeddingModel || records[0].CompletionTokens != 0 {
		t.Fatalf("用量流水不正确: %+v", records[0])
	}
	config, err := service.loadBuiltinAIConfig()
	if err != nil || config.DefaultModel != "first-model" || len(config.Models) != 2 {
		t.Fatal("向量模型污染了聊天模型列表")
	}
}

// TestEmbeddingPrices 验证小数分价格不被截断、输出仅按真实Token、极小用量向上取整。
func TestEmbeddingPrices(t *testing.T) {
	for _, item := range []struct {
		input, output int
		want          int64
	}{{1000000, 0, 1250}, {0, 1000000, 0}, {1000000, 1000000, 1250}, {1, 0, 1}, {0, 0, 0}} {
		got, err := embeddingCostUnits(item.input, item.output)
		if err != nil || got != item.want {
			t.Fatalf("%+v得到%d,err=%v", item, got, err)
		}
	}
	if _, err := embeddingCostUnits(-1, 0); err == nil {
		t.Fatal("负数用量未拒绝")
	}
}

// TestEmbeddingsRejectInvalidRequests 验证未授权、额外字段、尾随内容、错误模型、超额批次和非文字输入不会调用上游。
func TestEmbeddingsRejectInvalidRequests(t *testing.T) {
	called := 0
	service, wallet := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		called++
		return embeddingHTTPResponse(embeddingResultForTest(0)), nil
	}))
	tooMany, _ := json.Marshal(map[string]any{"input": strings.Split(strings.Repeat("段,", 20)+"段", ",")})
	for _, body := range []string{`{"input":""}`, `{"input":null}`, `{"input":[1]}`, `{"input":["简历",null]}`, `{"input":"简历","stream":true}`, `{"input":"简历"} {}`, `{"input":"简历","model":"chat-model"}`, `{"input":"简历","encoding_format":"base64"}`, string(tooMany)} {
		req := httptest.NewRequest(http.MethodPost, "/api/ai-compatible/v1/embeddings", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer embedding-user-key")
		res := httptest.NewRecorder()
		service.Embeddings(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("输入%s状态%d", body, res.Code)
		}
	}
	res := httptest.NewRecorder()
	service.Embeddings(res, httptest.NewRequest(http.MethodPost, "/api/ai-compatible/v1/embeddings", strings.NewReader(`{"input":"简历"}`)))
	if res.Code != http.StatusUnauthorized || called != 0 {
		t.Fatalf("授权失败仍调用: %d次", called)
	}
	if balance, _ := wallet.BalanceUnits("embedding@example.com"); balance != 100000 {
		t.Fatal("输入错误仍然扣款")
	}
}

// TestEmbeddingsRejectUntrustedConfig 验证外部域名、伪站内地址、停用及本人Key不匹配时不会调用向量模型。
func TestEmbeddingsRejectUntrustedConfig(t *testing.T) {
	called := 0
	service, _ := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) { called++; return nil, errors.New("不应调用") }))
	for _, base := range []string{"https://external.test/v1", "https://goodhr.example.com.evil.test/v1", "https://external.test/goodhr.example.com/v1"} {
		_, _ = service.aiConfigs.SaveUserConfig("embedding@example.com", AIConfig{BaseURL: base, APIKey: "embedding-user-key", Enabled: true})
		if _, err := service.EmbedForUser(context.Background(), "embedding@example.com", []string{"经历"}); err == nil {
			t.Fatal("外部配置获准使用内置向量")
		}
	}
	_, _ = service.aiConfigs.SaveUserConfig("embedding@example.com", AIConfig{BaseURL: "https://goodhr.example.com/api/ai", APIKey: "another-key", Enabled: true})
	if _, err := service.EmbedForUser(context.Background(), "embedding@example.com", []string{"经历"}); err == nil {
		t.Fatal("未核对Key获准调用")
	}
	if called != 0 {
		t.Fatal("无权配置调用了上游")
	}
}

// TestEmbeddingResponseValidation 验证缺失用量、错误模型、索引重复、空向量及错误维度不进入账本。
func TestEmbeddingResponseValidation(t *testing.T) {
	for _, kind := range []string{"usage", "model", "index", "dimension", "zero", "usage_mismatch", "negative", "http", "json", "null_number", "network", "balance"} {
		t.Run(kind, func(t *testing.T) {
			service, wallet := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				result := embeddingResultForTest(0)
				switch kind {
				case "usage":
					result.Usage.PromptTokens = nil
				case "model":
					result.Model = "other-model"
				case "index":
					result.Data[0].Index = nil
				case "dimension":
					result.Data[0].Embedding = []float64{1}
				case "zero":
					result.Data[0].Embedding = make([]float64, 1024)
				case "usage_mismatch":
					value := 7
					result.Usage.TotalTokens = &value
				case "negative":
					value := -1
					result.Usage.PromptTokens = &value
				case "http":
					return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("upstream-secret"))}, nil
				case "json":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not JSON upstream-secret"))}, nil
				case "null_number":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"` + builtinEmbeddingModel + `","data":[{"index":0,"embedding":[null,1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))}, nil
				case "network":
					return nil, errors.New("upstream-secret")
				}
				return embeddingHTTPResponse(result), nil
			}))
			if kind == "balance" {
				_, _ = wallet.AdjustBalance(AIWalletRecord{UserEmail: "embedding@example.com", ChangeUnits: -100000, Category: "ai_usage"})
			}
			before, _ := wallet.BalanceUnits("embedding@example.com")
			_, err := service.EmbedForUser(context.Background(), "embedding@example.com", []string{"经历"})
			after, _ := wallet.BalanceUnits("embedding@example.com")
			if err == nil || before != after || strings.Contains(err.Error(), "upstream-secret") {
				t.Fatalf("异常响应被使用或泄露：%v", err)
			}
		})
	}
}

// TestEmbeddingEndpointNormalization 验证兼容路径与查询参数保留，拒绝用户身份片段。
func TestEmbeddingEndpointNormalization(t *testing.T) {
	for _, item := range []struct{ input, want string }{{"https://example.test/compatible-mode/v1/chat/completions", "https://example.test/compatible-mode/v1/embeddings"}, {"https://example.test/v1", "https://example.test/v1/embeddings"}, {"https://example.test", "https://example.test/v1/embeddings"}, {"https://example.test/v1/embeddings?version=1", "https://example.test/v1/embeddings?version=1"}} {
		got, err := normalizeAIEmbeddingsURL(item.input)
		if err != nil || got != item.want {
			t.Fatalf("地址转换=%s,err=%v", got, err)
		}
	}
	if _, err := normalizeAIEmbeddingsURL("https://key@example.test/v1"); err == nil {
		t.Fatal("含用户身份的地址未拒绝")
	}
}
