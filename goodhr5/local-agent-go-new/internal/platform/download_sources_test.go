// Package platform 文件作用：验证招聘来源识别按真实URL主机匹配，未知CDN和伪装域名不能授权来源。
package platform

import "testing"

// TestDownloadSourceDomains 覆盖四个平台、大小写、空referrer、恶意userinfo与签名清理。
func TestDownloadSourceDomains(t *testing.T) {
	for _, item := range []struct{ page, download, want string }{{"https://www.zhipin.com/web?key=secret", "", "boss"}, {"https://h.liepin.com/resume", "", "liepin"}, {"https://rd6.zhaopin.com/resume", "", "zhaopin"}, {"https://ehire.51job.com/resume", "", "51job"}, {"", "https://download.51job.cn/a", "51job"}, {"", "https://cdn.example.invalid/a", ""}, {"https://zhipin.com.evil.invalid/a", "", ""}, {"https://zhipin.com@evil.invalid/a", "", ""}, {"file:///zhipin.com", "", ""}, {"https://WWW.ZHIPIN.COM/a?key=x#private", "", "boss"}} {
		platform, source := DownloadSource(item.page, item.download)
		if platform != item.want {
			t.Fatalf("来源=%s want=%s", platform, item.want)
		}
		if item.want != "" && source == "" {
			t.Fatal("缺少来源")
		}
	}
}
