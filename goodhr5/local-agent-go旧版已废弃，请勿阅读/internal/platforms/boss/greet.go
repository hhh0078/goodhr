// Package boss 文件作用：承载 greet.go 对应的平台职责实现。
package boss

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// GreetCandidate 执行 Boss 打招呼。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	exec.Log("info", fmt.Sprintf("准备调用打招呼接口：name=%s", candidateName(candidate)))
	payload := bossCandidateVisiblePayload(cfg, candidate)
	payload["debug_stage"] = "greet-before"
	if _, err := exec.Post(ctx, "/api/v1/boss/candidates/visible", payload); err != nil {
		return err
	}
	payload["debug_stage"] = "greet-click"
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/greet", payload)
	return err
}
