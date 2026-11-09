// Package lifecycle 文件作用：验证父子时限、主动停止和浏览器错误在落库及私密日志投影时仍能区分。
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// TestFailureSourcesSurviveLogUpload 验证外层候选人时限不能盖掉内层正在等待的 AI 步骤，旧自动回复隐私规则继续隐藏正文。
func TestFailureSourcesSurviveLogUpload(t *testing.T) {
	ai := failurediag.Detail{Source: "AI", Operation: "最终评分", Phase: "等待首段内容", Reason: "上层步骤的时限已到"}
	global := failurediag.Detail{Source: "岗位流程", Operation: "处理单个候选人", Reason: "单个候选人已用完五分钟预算", LimitMS: 300000}
	err := failurediag.Wrap(fmt.Errorf("候选人处理结束：%w", failurediag.Wrap(context.DeadlineExceeded, ai)), global)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("诊断包装改变了原来的超时类型")
	}
	l := NewTaskLogger(nil, nil)
	out := l.taskLogOutput("failed", 310000, err)
	var payload map[string]json.RawMessage
	if json.Unmarshal(out, &payload) != nil {
		t.Fatal("未生成岗位出参")
	}
	payload["body"] = json.RawMessage(`"私密正文"`)
	raw, _ := json.Marshal(payload)
	payload["body"], _ = json.Marshal(strings.Repeat("x", 16000))
	large, _ := json.Marshal(payload)
	for _, uploaded := range []json.RawMessage{uploadLogObject(out), uploadLogObject(large), uploadPrivateDiagnostic(raw)} {
		var result struct {
			Failures []failurediag.Detail `json:"failure_diagnostics"`
		}
		if err := json.Unmarshal(uploaded, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Failures) != 2 || result.Failures[0] != global || result.Failures[1] != ai || strings.Contains(string(uploaded), "私密正文") {
			t.Fatalf("分享丢失证据顺序或出现正文：%s", uploaded)
		}
	}
	for _, test := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "发起方未知"}, {context.DeadlineExceeded, "无法确认由哪一层设置"},
	} {
		if got := failurediag.Summary(taskFailureDetails(test.err, nil)); !strings.Contains(got, test.want) {
			t.Fatalf("未保留未知来源：%s", got)
		}
	}
}

// TestDetailReadEvidence 核对同一句“暂时没读到”按实际回执可以区分找不到、已找到但读取失败和上层到期。
func TestDetailReadEvidence(t *testing.T) {
	for _, tc := range []struct{ phase, reason, code, want string }{
		{"locate_read_target", "worker_error", "ELEMENT_NOT_FOUND", "未能定位符合条件的页面元素"},
		{"read_value", "worker_error", "INTERNAL_ERROR", "已定位页面元素，但未能取回内容"},
		{"read_value", "parent_deadline", "", "上层步骤时限已到"},
	} {
		browser := &browserDiagnostic{Request: &contract.RequestDiagnostic{WorkerPhase: tc.phase, Phase: "worker_response", Reason: tc.reason, ErrorCode: tc.code}}
		details := taskFailureDetails(errors.New("暂时没读到"), browser)
		if len(details) != 1 || !strings.Contains(details[0].Reason, tc.want) || details[0].Operation == "" {
			t.Fatalf("未区分动作 %s 和原因 %s：%+v", tc.phase, tc.reason, details)
		}
	}
}
