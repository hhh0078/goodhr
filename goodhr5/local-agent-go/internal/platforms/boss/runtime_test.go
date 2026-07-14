// Package boss 测试 Boss 平台运行时的候选人解析规则。
package boss

import (
	"context"
	"testing"

	"goodhr5/local-agent-go/internal/platformcore"
)

// TestCandidateFingerprintUsesOnlyNameAndAge 验证 Boss 候选人 ID 只由姓名和年龄决定。
func TestCandidateFingerprintUsesOnlyNameAndAge(t *testing.T) {
	runtime := NewRuntime()
	first := platformcore.Candidate{
		"candidate_name": "范召",
		"raw_text":       "范召 29岁 本科 5年 带货主播",
		"fields":         map[string]any{"name": "范召", "basic_info": "29岁 本科 5年 带货主播"},
	}
	second := platformcore.Candidate{
		"candidate_name": "范召",
		"raw_text":       "范召 29岁 大专 8年 直播运营",
		"fields":         map[string]any{"name": "范召", "basic_info": "29岁 大专 8年 直播运营"},
	}
	if runtime.CandidateFingerprint(first) != "boss_范召_29" {
		t.Fatalf("候选人 ID 应只包含姓名年龄：%s", runtime.CandidateFingerprint(first))
	}
	if runtime.CandidateFingerprint(first) != runtime.CandidateFingerprint(second) {
		t.Fatalf("同名同年龄应生成相同 ID：first=%s second=%s", runtime.CandidateFingerprint(first), runtime.CandidateFingerprint(second))
	}
}

// TestCandidateFingerprintRequiresAge 验证缺少年龄时不生成 Boss 候选人 ID。
func TestCandidateFingerprintRequiresAge(t *testing.T) {
	runtime := NewRuntime()
	candidate := platformcore.Candidate{"candidate_name": "范召", "raw_text": "范召 本科 5年"}
	if id := runtime.CandidateFingerprint(candidate); id != "" {
		t.Fatalf("缺少年龄时不应生成 ID：%s", id)
	}
}

// TestSelectPositionUsesSearchInputFirst 验证 Boss 切换岗位优先使用岗位搜索框。
func TestSelectPositionUsesSearchInputFirst(t *testing.T) {
	runtime := NewRuntime()
	exec := &selectPositionSearchExecutor{}
	cfg := map[string]any{
		"position": map[string]any{
			"switchBtn":   map[string]any{"target_classes": []any{[]any{".job-switch"}}},
			"searchInput": map[string]any{"target_classes": []any{[]any{".ipt.chat-job-search"}}},
			"list":        map[string]any{"target_classes": []any{[]any{".job-list"}}},
			"item":        map[string]any{"target_classes": []any{[]any{".job-item"}}},
			"itemText":    map[string]any{"target_classes": []any{[]any{".label"}}},
		},
	}
	if err := runtime.SelectPosition(context.Background(), exec, cfg, "销售顾问"); err != nil {
		t.Fatalf("切换岗位不应失败：%v", err)
	}
	wantPaths := []string{"/api/v1/page/click", "/api/v1/page/type", "/api/v1/page/find-elements", "/api/v1/page/click"}
	if len(exec.calls) != len(wantPaths) {
		t.Fatalf("调用次数不对：got=%d want=%d calls=%v", len(exec.calls), len(wantPaths), exec.calls)
	}
	for index, want := range wantPaths {
		if exec.calls[index].path != want {
			t.Fatalf("第 %d 次调用路径不对：got=%s want=%s", index+1, exec.calls[index].path, want)
		}
	}
	typePayload := exec.calls[1].payload
	if typePayload["text"] != "销售顾问" {
		t.Fatalf("岗位搜索关键词不对：%v", typePayload["text"])
	}
	clickPayload := exec.calls[3].payload
	if clickPayload["element_ref"] != "job-ref-1" {
		t.Fatalf("应该点击搜索结果第一个元素引用：%v", clickPayload["element_ref"])
	}
}

type selectPositionSearchCall struct {
	path    string
	payload map[string]any
}

type selectPositionSearchExecutor struct {
	calls []selectPositionSearchCall
}

// Post 记录 Boss 切换岗位时调用的 Worker 接口，并返回模拟搜索结果。
func (e *selectPositionSearchExecutor) Post(ctx context.Context, path string, payload any) (map[string]any, error) {
	data, _ := payload.(map[string]any)
	e.calls = append(e.calls, selectPositionSearchCall{path: path, payload: data})
	if path == "/api/v1/page/find-elements" {
		return map[string]any{
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"index":       0,
						"element_ref": "job-ref-1",
						"fields":      map[string]any{"position_name": "销售顾问"},
					},
				},
			},
		}, nil
	}
	return map[string]any{"data": map[string]any{}}, nil
}

// Log 接收测试中的任务日志。
func (e *selectPositionSearchExecutor) Log(level string, message string) {}

// Delay 模拟业务等待。
func (e *selectPositionSearchExecutor) Delay(ctx context.Context, label string, seconds float64) error {
	return nil
}

// TestCleanCandidateDetailText 验证 Boss 平台附加分析内容不会进入候选人详情。
func TestCleanCandidateDetailText(t *testing.T) {
	runtime := NewRuntime()
	raw := "解婷 25岁 大专 工作经历 主播\n牛人分析器\nVIP专享 同类牛人\n平台隐私声明"
	cleaned := runtime.CleanCandidateDetailText(raw)
	if cleaned != "解婷 25岁 大专 工作经历 主播" {
		t.Fatalf("cleaned = %q", cleaned)
	}
}
