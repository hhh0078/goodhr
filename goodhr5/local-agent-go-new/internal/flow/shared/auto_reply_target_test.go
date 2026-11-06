// Package shared 本文件验证单会话限制必须具备稳定身份，且进程强制范围不能被普通启动请求绕过。
package shared

import "testing"

// TestResolveAutoReplyTargetForcesProcessScope 验证省略请求目标仍受环境限制，覆盖目标和无效配置均被拒绝。
func TestResolveAutoReplyTargetForcesProcessScope(t *testing.T) {
	forced := AutoReplyTarget{CandidateName: "目标候选人", PlatformThreadID: "thread", PlatformCandidateID: "candidate"}
	t.Setenv(AutoReplyTargetEnvironment, `{"candidate_name":"目标候选人","platform_thread_id":"thread","platform_candidate_id":"candidate"}`)
	for _, requested := range []*AutoReplyTarget{nil, &forced} {
		got, err := ResolveAutoReplyTarget("auto_reply", requested)
		if err != nil || got == nil || *got != forced {
			t.Fatal("普通启动未应用进程限制", got, err)
		}
	}
	other := forced
	other.PlatformCandidateID = "someone-else"
	if _, err := ResolveAutoReplyTarget("auto_reply", &other); err == nil {
		t.Fatal("请求覆盖了进程强制限制")
	}
	for _, raw := range []string{`{}`, `null`, `{"candidate_name":"目标候选人"}`, `{"candidate_name":"目标候选人","platform_thread_id":"thread","platform_candidate_id":"candidate","unexpected":true}`, `{} {}`} {
		t.Setenv(AutoReplyTargetEnvironment, raw)
		if _, err := ResolveAutoReplyTarget("auto_reply", nil); err == nil {
			t.Fatal("无效进程限制被当作无限制", raw)
		}
	}
	t.Setenv(AutoReplyTargetEnvironment, "")
	if got, err := ResolveAutoReplyTarget("auto_reply", nil); err != nil || got != nil {
		t.Fatal("未启用限制时改变了正常行为", got, err)
	}
}

// TestValidateAutoReplyTargetRequiresStableIdentity 验证仅姓名、推测编号和用于打招呼的限制不会获得发送许可。
func TestValidateAutoReplyTargetRequiresStableIdentity(t *testing.T) {
	for _, target := range []AutoReplyTarget{
		{CandidateName: "目标候选人"},
		{CandidateName: "目标候选人", PlatformThreadID: "thread"},
		{CandidateName: "目标候选人", PlatformThreadID: "local:guess", PlatformCandidateID: "candidate"},
		{CandidateName: " 目标候选人", PlatformThreadID: "thread", PlatformCandidateID: "candidate"},
	} {
		if err := ValidateAutoReplyTarget("auto_reply", &target); err == nil {
			t.Fatal("缺失或不可靠身份没有被拒绝", target)
		}
	}
	if err := ValidateAutoReplyTarget("greeting", &AutoReplyTarget{}); err == nil {
		t.Fatal("打招呼任务接受了自动回复限制")
	}
}
