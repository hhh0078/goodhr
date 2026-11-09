// Package auto_reply 本文件验证编辑资料后可以基于新来信重开原问题，不能引用别的岗位或伪造原话。
package auto_reply

import (
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"testing"
)

// TestResolvedQuestionCanReopenWithNewSource 验证历史resolved状态不能阻止当前资料缺失时使用原题去重。
func TestResolvedQuestionCanReopenWithNewSource(t *testing.T) {
	input := ReplyContext{KnownQuestions: []knownRecruitmentQuestion{{ID: "known", Status: "resolved", Question: "公司地址在哪里？"}}, PendingMessages: []cloud.AutoReplyMessage{{Direction: "candidate", TextContent: "请问公司现在在哪里办公？", PlatformMessageID: "new-inbound"}}}
	args := notifyHRToolArgs{ReasonKey: "missing_recruitment_info", Question: "公司现在在哪里办公", ExistingQuestionID: "known"}
	rows, err := verifiedHandoffQuestions(input, args)
	if err != nil || len(rows) != 1 || rows[0].sourceKey != "new-inbound" {
		t.Fatal("历史题未能关联新的真实来信", rows, err)
	}
	args.ExistingQuestionID = "other-position-question"
	if _, err = verifiedHandoffQuestions(input, args); err == nil {
		t.Fatal("接受了其它岗位的问题编号")
	}
	args.ExistingQuestionID = "known"
	args.Question = "待遇有多少"
	if _, err = verifiedHandoffQuestions(input, args); err == nil {
		t.Fatal("接受了候选人没有问的文字")
	}
}
