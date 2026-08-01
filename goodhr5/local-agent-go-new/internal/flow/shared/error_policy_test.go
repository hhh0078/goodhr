// Package shared 文件作用：验证连续同类错误达到三次才停止，并在成功后清零。
package shared

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// TestConsecutiveErrorPolicyStopsAtThree 验证变化的超时数字不会打断同类错误计数。
func TestConsecutiveErrorPolicyStopsAtThree(t *testing.T) {
	policy := &ConsecutiveErrorPolicy{}
	for index, timeout := range []string{"1000ms", "2000ms", "3000ms"} {
		err := policy.Record(errors.New("open_detail：selector timeout " + timeout))
		if index < 2 && err != nil {
			t.Fatalf("第 %d 次错误不应停止：%v", index+1, err)
		}
		if index == 2 && (err == nil || !strings.Contains(err.Error(), "连续 3 次")) {
			t.Fatalf("第三次同类错误应停止：%v", err)
		}
	}
}

// TestConsecutiveErrorPolicyReset 验证中间成功一次后连续错误从第一次重新计算。
func TestConsecutiveErrorPolicyReset(t *testing.T) {
	policy := &ConsecutiveErrorPolicy{}
	_ = policy.Record(errors.New("send_reply：selector timeout"))
	_ = policy.Record(errors.New("send_reply：selector timeout"))
	policy.Reset()
	if err := policy.Record(errors.New("send_reply：selector timeout")); err != nil {
		t.Fatalf("成功清零后第一次错误不应停止：%v", err)
	}
}

// TestConsecutiveErrorPolicyUsesWorkerFields 验证候选人姓名不同但操作位置相同的 Worker 错误会连续计数。
func TestConsecutiveErrorPolicyUsesWorkerFields(t *testing.T) {
	policy := &ConsecutiveErrorPolicy{}
	for index, name := range []string{"韩女士", "李女士", "沈女士"} {
		workerErr := &contract.WorkerError{Body: contract.WorkerErrorBody{
			Code:    "ELEMENT_NOT_FOUND",
			Action:  "element.scroll",
			Step:    "find",
			Message: fmt.Sprintf("候选人 %s 暂时没找到", name),
		}}
		err := policy.Record(fmt.Errorf("滚动到候选人失败：%w", workerErr))
		if index < 2 && err != nil {
			t.Fatalf("第 %d 次同类 Worker 错误不应停止：%v", index+1, err)
		}
		if index == 2 && (err == nil || !strings.Contains(err.Error(), "连续 3 次")) {
			t.Fatalf("第三次同类 Worker 错误应停止：%v", err)
		}
	}
}
