// Package greeting 文件作用：验证 AI 完整结果不会阻塞页面流程，并会在后台同步结构化候选人。
package greeting

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestFinishCandidateEvaluationAsyncWaitsInBackground 验证完整 AI 输出在后台等待，完成后才同步云端。
func TestFinishCandidateEvaluationAsyncWaitsInBackground(t *testing.T) {
	received := make(chan cloud.CandidateUpload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload cloud.CandidateUpload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("解析候选人失败：%v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	final := make(chan ai.EvaluationResult, 1)
	flow := &Flow{Cloud: cloud.New(server.URL)}
	prepared := shared.PreparedTask{
		Request: shared.StartRequest{
			TaskID: "task-1",
			Token:  "token",
		},
		Position: cloud.PositionSnapshot{
			ID:         "position-1",
			PlatformID: "hliepin",
			CommonConfig: cloud.PositionCommonConfig{
				OutputStructuredResume: true,
			},
		},
	}
	candidate := model.Candidate{
		Name:    "候选人甲",
		Summary: "基础信息",
		Fields:  map[string]string{"platform_candidate_id": "candidate-1"},
	}

	startedAt := time.Now()
	flow.finishCandidateEvaluationAsync(
		prepared,
		candidate,
		"greeted",
		ai.Evaluation{
			Decision: ai.Decision{Score: 86, Reason: "提前评分"},
			Final:    final,
		},
		nil,
	)
	if time.Since(startedAt) > 50*time.Millisecond {
		t.Fatal("后台等待完整 AI 输出不应该阻塞页面流程")
	}
	select {
	case <-received:
		t.Fatal("完整 AI 输出返回前不应该同步云端")
	default:
	}

	final <- ai.EvaluationResult{Decision: ai.Decision{
		Score:  86,
		Reason: "匹配岗位",
		Resume: &cloud.StructuredCandidate{
			CandidateName: "候选人甲",
			WorkRegion:    "上海",
		},
	}}
	close(final)

	select {
	case payload := <-received:
		if payload.CandidateName != "候选人甲" || payload.WorkRegion != "上海" {
			t.Fatalf("结构化候选人字段不正确：%+v", payload)
		}
		if payload.AIGreetScore == nil || *payload.AIGreetScore != 86 {
			t.Fatalf("最终评分不正确：%+v", payload.AIGreetScore)
		}
	case <-time.After(time.Second):
		t.Fatal("完整 AI 输出完成后没有异步同步云端")
	}
}
