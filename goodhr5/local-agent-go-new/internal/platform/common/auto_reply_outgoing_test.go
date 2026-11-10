// Package common 本文件验证平台出站正文定稿保留内部信息，仅按显式行为将换行转为空格。
package common

import (
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestNormalizeAutoReplyOutgoingText 验证单行规则、旧配置默认、多种空白及重复定稿的一致性。
func TestNormalizeAutoReplyOutgoingText(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		singleLine        bool
	}{
		{name: "空正文"},
		{name: "纯空白", input: " \r\n\t ", singleLine: true},
		{name: "旧平台保留换行", input: " \t岗位\n公司 \r\n", want: "岗位\n公司"},
		{name: "普通正文", input: "  成都 招聘  ", want: "成都 招聘", singleLine: true},
		{name: "换行留分隔", input: "成都\n待确认", want: "成都 待确认", singleLine: true},
		{name: "Windows换行只换一次", input: "成都\r\n待确认", want: "成都 待确认", singleLine: true},
		{name: "连续换行逐个替换", input: "成都\n\n待确认", want: "成都  待确认", singleLine: true},
		{name: "已有空格不合并", input: "成都  \n 待确认", want: "成都    待确认", singleLine: true},
		{name: "内部制表符及孤立回车保留", input: "成都\t岗位\r信息", want: "成都\t岗位\r信息", singleLine: true},
		{name: "其它平台保留内部空格", input: "\n 成都  \t岗位\n公司 \n", want: "成都  \t岗位\n公司"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := model.Config{Behavior: model.Behavior{AutoReplySingleLineMessages: tc.singleLine}}
			got := NormalizeAutoReplyOutgoingText(cfg, tc.input)
			if got != tc.want {
				t.Fatalf("正文定稿变化不符：got=%q want=%q", got, tc.want)
			}
			if repeated := NormalizeAutoReplyOutgoingText(cfg, got); repeated != got {
				t.Fatalf("重复定稿改变正文：before=%q after=%q", got, repeated)
			}
		})
	}
}
