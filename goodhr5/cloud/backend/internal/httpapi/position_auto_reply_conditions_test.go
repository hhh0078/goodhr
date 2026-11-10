// Package httpapi 本文件验证自动生成条件的原文约束、优先项、去重与登录边界。
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAutoReplyConditionSuggestionsRequireSource 验证生成的条件必须能追到岗位原句，并保留优先项性质。
func TestAutoReplyConditionSuggestionsRequireSource(t *testing.T) {
	source := "本科及以上。熟悉Go优先。工作地点成都。上班时间尚未确认。"
	for _, tc := range []struct {
		name, raw string
		want      int
		invalid   bool
	}{
		{"有效条件", `{"suggestions":[{"content":"本科及以上","source":"本科及以上。","type":"required"}]}`, 1, false},
		{"优先项", `{"suggestions":[{"content":"熟悉Go","source":"熟悉Go优先。","type":"required"}]}`, 1, false},
		{"编造", `{"suggestions":[{"content":"三年经验","source":"三年经验","type":"required"}]}`, 0, true},
		{"原句不含内容", `{"suggestions":[{"content":"硕士","source":"本科及以上。","type":"required"}]}`, 0, true},
		{"重复", `{"suggestions":[{"content":"本科及以上","source":"本科及以上。","type":"required"},{"content":"本科及以上","source":"本科及以上。","type":"required"}]}`, 1, false},
		{"岗位信息未确认", `{"suggestions":[{"content":"上班时间尚未确认","source":"上班时间尚未确认。","type":"confirm"}]}`, 0, false},
		{"空建议", `{"suggestions":[]}`, 0, false},
		{"坏响应", `{"suggestions":"本科"}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, err := parseAutoReplyConditionSuggestions(tc.raw, source)
			if (err != nil) != tc.invalid {
				t.Fatalf("err=%v", err)
			}
			if len(items) != tc.want {
				t.Fatalf("建议数量=%d", len(items))
			}
			if tc.name == "优先项" && items[0].Type != "bonus" {
				t.Fatal("把优先变成了硬门槛")
			}
		})
	}
}

// TestAutoReplyConditionSuggestionsRejectInvalidMethods 验证普通 GET 不会发起付费模型请求。
func TestAutoReplyConditionSuggestionsRejectInvalidMethods(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.suggestAutoReplyConditions(w, httptest.NewRequest(http.MethodGet, "/api/positions/test/auto-reply/condition-suggestions", strings.NewReader("{}")))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", w.Code)
	}
}
