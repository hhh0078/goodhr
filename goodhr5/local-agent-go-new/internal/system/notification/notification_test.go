// Package notification 文件作用：测试下载提示窗输出解析，保证用户选择不会被额外输出带偏。
package notification

import "testing"

// TestParseDownloadAction 验证能从弹窗脚本输出里正确解析用户选择。
func TestParseDownloadAction(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   DownloadAction
	}{
		{name: "打开文件", output: "open\r\n", want: DownloadOpen},
		{name: "打开文件夹", output: "reveal\r\n", want: DownloadReveal},
		{name: "先放着", output: "dismiss\r\n", want: DownloadDismiss},
		{name: "超时自动关闭", output: "", want: DownloadDismiss},
		{name: "带额外输出", output: "warning line\r\nopen\r\n", want: DownloadOpen},
		{name: "无法识别", output: "something-else\r\n", want: DownloadDismiss},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := parseDownloadAction(item.output); got != item.want {
				t.Fatalf("parseDownloadAction(%q) = %q, 期望 %q", item.output, got, item.want)
			}
		})
	}
}
