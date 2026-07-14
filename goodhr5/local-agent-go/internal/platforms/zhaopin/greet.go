// Package zhaopin 文件作用：承载 greet.go 对应的平台职责实现。
package zhaopin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// GreetCandidate 执行智联招聘候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	exec.Log("info", fmt.Sprintf("准备调用打招呼接口：name=%s", candidateName(candidate)))
	payload := zhaopinCandidateVisiblePayload(cfg, candidate)
	payload["debug_stage"] = "greet-before"
	if _, err := exec.Post(ctx, "/api/v1/boss/candidates/visible", payload); err != nil {
		return err
	}
	payload["debug_stage"] = "greet-click"
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/greet", payload)
	return err
}
