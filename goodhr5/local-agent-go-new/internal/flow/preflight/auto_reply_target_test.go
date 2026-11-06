// Package preflight 本文件验证普通启动请求会自动继承进程单会话范围，显式覆盖范围会在检查阶段被拒绝。
package preflight

import (
	"context"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"testing"
)

// TestPreflightForcesAutoReplyTarget 验证所有自动回复启动入口在进入浏览器步骤前锁定同一目标。
func TestPreflightForcesAutoReplyTarget(t *testing.T) {
	t.Setenv(shared.AutoReplyTargetEnvironment, `{"candidate_name":"目标候选人","platform_thread_id":"thread","platform_candidate_id":"candidate"}`)
	prepared := &shared.PreparedTask{Request: shared.StartRequest{TaskID: "task", PositionID: "position", TaskType: "auto_reply", Token: "token", PlatformAccountID: "招聘账号"}}
	if err := validateRequest(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Request.AutoReplyTarget == nil || prepared.Request.AutoReplyTarget.PlatformCandidateID != "candidate" {
		t.Fatal("普通请求绕过进程限制")
	}
	prepared.Request.AutoReplyTarget.PlatformCandidateID = "other"
	if err := validateRequest(context.Background(), prepared); err == nil {
		t.Fatal("前置检查接受了范围覆盖")
	}
}
