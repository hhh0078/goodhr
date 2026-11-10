// Package auto_reply 本文件回归真实签收文案被跨句误判索号的问题，并验证同句真实索要与配置话术仍受保护。
package auto_reply

import (
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestNativeRequestAcknowledgementDoesNotJoinSentences 验证真实三轮答案签收手机号后谈简历，不会被跨句拼成再次索号。
func TestNativeRequestAcknowledgementDoesNotJoinSentences(t *testing.T) {
	config := cloud.PositionAutoReplyConfig{PhrasesVersion: "clause-regression", Phrases: []cloud.AutoReplyPhrase{{ID: "request_phone", Content: "方便留个手机号吗？"}, {ID: "request_wechat", Content: "方便交换微信吗？"}, {ID: "request_resume", Content: "方便发一份简历吗？"}}}
	input := ReplyContext{NativeInfoRequests: true, Position: cloud.AutoReplyPositionSnapshot{Config: config}, InfoContact: map[string]infoContactState{"phone": {Received: true}}}
	actual := []string{
		"好的，手机号收到了～工作地点在成都，具体工作内容是整理招聘资料、记录候选人沟通需求。简历您方便时发过来就行。",
		"好的，手机号收到了～工作地点在成都，具体工作内容是整理招聘资料、记录候选人沟通需求。简历您方便时发过来就行。",
		"好的，手机号这边收到了～工作地点在成都，具体工作内容是整理招聘资料、记录候选人沟通需求。简历您方便时发过来就行。",
	}
	for i, text := range actual {
		if asksForPhone(text, fixedReplyPhrase(config, "request_phone")) || asksForWechat(text, fixedReplyPhrase(config, "request_wechat")) {
			t.Fatalf("真实第%d轮签收答案被拼成索号或索微信", i+1)
		}
		if err := checkPhoneReply(phoneContactState{Received: true}, config, text); err != nil {
			t.Fatalf("真实第%d轮签收被已获手机号保护误拦：%v", i+1, err)
		}
		if err := validateNativeInfoProposal(input, model.CandidateInfoRequest{RequestWechat: true}, []ReplyMessage{{Type: "answer", Content: text}}); err != nil {
			t.Fatalf("真实第%d轮普通答案加原生微信申请被误拦：%v", i+1, err)
		}
	}
}

// TestInfoRequestsStayWithinTheirClause 验证签收与另一类索要分开识别，同句真正的请求仍被原生平台正文保护拦截。
func TestInfoRequestsStayWithinTheirClause(t *testing.T) {
	for _, tc := range []struct {
		text                  string
		phone, wechat, resume bool
	}{
		{text: "手机号收到了。方便发一份简历吗？", resume: true},
		{text: "手机号收到了，方便发一份简历吗？", resume: true},
		{text: "简历收到了，方便交换微信吗？", wechat: true},
		{text: "微信收到了；可以提供手机号吗？", phone: true},
		{text: "手机号收到了，明天方便参加面试吗？"},
		{text: "手机号收到了～明天可以到公司面试。"},
		{text: "简历收到了\n能告诉我您的手机号吗？", phone: true},
		{text: "方便留个手机号吗？", phone: true},
		{text: "请问联系电话是多少？", phone: true},
		{text: "联系电话可以发我吗？", phone: true},
		{text: "请告诉我您的手机号。", phone: true},
		{text: "方便给个微信吗？", wechat: true},
		{text: "加个微信可以吗？", wechat: true},
		{text: "能发一份简历吗？", resume: true},
		{text: "请提供简历。", resume: true},
	} {
		t.Run(tc.text, func(t *testing.T) {
			if phone, wechat, resume := asksForPhone(tc.text, ""), asksForWechat(tc.text, ""), asksForResume(tc.text, ""); phone != tc.phone || wechat != tc.wechat || resume != tc.resume {
				t.Fatalf("分句识别不符：got=%v/%v/%v want=%v/%v/%v", phone, wechat, resume, tc.phone, tc.wechat, tc.resume)
			}
			input := ReplyContext{NativeInfoRequests: true}
			err := validateNativeInfoProposal(input, model.CandidateInfoRequest{}, []ReplyMessage{{Type: "answer", Content: tc.text}})
			if blocked := err != nil; blocked != (tc.phone || tc.wechat || tc.resume) {
				t.Fatal("原生平台正文保护误拦签收或漏掉真实索要", err)
			}
		})
	}
}

// TestConfiguredInfoRequestKeepsExactPriority 验证完整配置短句优先，即使横跨分句或不包含默认词也按明确话术识别。
func TestConfiguredInfoRequestKeepsExactPriority(t *testing.T) {
	configured := "先小声问一下。请分享资料。"
	for _, asks := range []func(string, string) bool{asksForPhone, asksForWechat, asksForResume} {
		if !asks("您好，"+configured, configured) || asks("请分享资料。", configured) {
			t.Fatal("配置的精确短句匹配被分句规则覆盖")
		}
	}
}
